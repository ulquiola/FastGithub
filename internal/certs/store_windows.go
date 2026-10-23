//go:build windows

package certs

import (
	"errors"
	"fmt"
	"syscall"
	"unsafe"
)

// CryptoAPI 常量与 CertFindCertificateInStore 的查找类型。
const (
	certStoreProvSystemW = 10         // CERT_STORE_PROV_SYSTEM_W
	certFindSha1Hash     = 0x00010000 // CERT_FIND_SHA1_HASH（传给 pvFindPara 的是 CRYPT_HASH_BLOB）

	x509AsnEncoding  = 0x00000001
	pkcs7AsnEncoding = 0x00010000

	certStoreCurrentUser  = 0x00010000 // CERT_SYSTEM_STORE_CURRENT_USER
	certStoreLocalMachine = 0x00020000 // CERT_SYSTEM_STORE_LOCAL_MACHINE
	// certStoreReadonly 是 CERT_STORE_READONLY_FLAG：不加的话 CertOpenStore 默认申请
	// 读写权限，普通用户查本机 ROOT 存储会直接 Access denied。
	certStoreReadonly = 0x00008000

	storeNameRoot = "ROOT"
)

var (
	crypt32                        = syscall.NewLazyDLL("crypt32.dll")
	procCertOpenStore              = crypt32.NewProc("CertOpenStore")
	procCertFindCertificateInStore = crypt32.NewProc("CertFindCertificateInStore")
	procCertFreeCertificateContext = crypt32.NewProc("CertFreeCertificateContext")
	procCertCloseStore             = crypt32.NewProc("CertCloseStore")
)

// cryptHashBlob 对应 Win32 CRYPT_HASH_BLOB。
type cryptHashBlob struct {
	CbData uint32
	PbData *byte
}

// systemStoreStatus 用 CryptoAPI 直接查根存储，避免每次探测都拉起一个 certutil
// 子进程（UI 轮询时每 5 秒一个）。先查本机存储再查当前用户存储，与 macOS 的
// 系统钥匙串 → 登录钥匙串顺序一致。
func systemStoreStatus(hash []byte) (TrustStatus, error) {
	if len(hash) == 0 {
		return TrustStatus{}, errors.New("证书指纹为空")
	}
	for _, scoped := range []struct {
		flag  uintptr
		scope string
	}{
		{certStoreLocalMachine, "system"},
		{certStoreCurrentUser, "user"},
	} {
		found, err := rootStoreContains(scoped.flag, hash)
		if err != nil {
			return TrustStatus{}, err
		}
		if found {
			return TrustStatus{Installed: true, Scope: scoped.scope}, nil
		}
	}
	return TrustStatus{Scope: "none"}, nil
}

// rootStoreContains 报告给定作用域的 ROOT 存储里是否存在该 SHA-1 指纹的证书。
func rootStoreContains(scopeFlag uintptr, hash []byte) (bool, error) {
	name, err := syscall.UTF16PtrFromString(storeNameRoot)
	if err != nil {
		return false, err
	}
	store, _, errno := procCertOpenStore.Call(
		certStoreProvSystemW,
		0,
		0,
		scopeFlag|certStoreReadonly,
		uintptr(unsafe.Pointer(name)),
	)
	if store == 0 {
		if errno == nil {
			errno = syscall.EINVAL
		}
		return false, fmt.Errorf("打开根证书存储失败: %w", errno)
	}
	defer procCertCloseStore.Call(store, 0)

	blob := cryptHashBlob{CbData: uint32(len(hash)), PbData: &hash[0]}
	ctx, _, _ := procCertFindCertificateInStore.Call(
		store,
		uintptr(x509AsnEncoding|pkcs7AsnEncoding),
		0,
		certFindSha1Hash,
		uintptr(unsafe.Pointer(&blob)),
		0,
	)
	if ctx == 0 {
		// 找不到就是未安装；CertFindCertificateInStore 无更细的错误语义。
		return false, nil
	}
	procCertFreeCertificateContext.Call(ctx)
	return true, nil
}
