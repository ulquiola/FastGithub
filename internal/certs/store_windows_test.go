//go:build windows

package certs

import (
	"bytes"
	"syscall"
	"testing"
	"unsafe"
)

const certSha1HashPropID = 3 // CERT_SHA1_HASH_PROP_ID

var (
	procCertEnumCertificatesInStore       = crypt32.NewProc("CertEnumCertificatesInStore")
	procCertGetCertificateContextProperty = crypt32.NewProc("CertGetCertificateContextProperty")
)

// firstMachineRootHash 从本机 ROOT 存储里取一张证书的 SHA-1 属性，作为正向对照。
func firstMachineRootHash(t *testing.T) []byte {
	t.Helper()
	name, err := syscall.UTF16PtrFromString(storeNameRoot)
	if err != nil {
		t.Fatal(err)
	}
	store, _, errno := procCertOpenStore.Call(certStoreProvSystemW, 0, 0, certStoreLocalMachine|certStoreReadonly, uintptr(unsafe.Pointer(name)))
	if store == 0 {
		t.Fatalf("打开本机 ROOT 存储失败: %v", errno)
	}
	defer procCertCloseStore.Call(store, 0)

	ctx, _, _ := procCertEnumCertificatesInStore.Call(store, 0)
	if ctx == 0 {
		t.Fatal("本机 ROOT 存储为空，无法做正向对照")
	}
	defer procCertFreeCertificateContext.Call(ctx)

	var size uint32
	if r, _, _ := procCertGetCertificateContextProperty.Call(ctx, certSha1HashPropID, 0, uintptr(unsafe.Pointer(&size))); r == 0 || size == 0 {
		t.Fatal("读取证书 SHA-1 属性的长度失败")
	}
	hash := make([]byte, size)
	if r, _, _ := procCertGetCertificateContextProperty.Call(ctx, certSha1HashPropID, uintptr(unsafe.Pointer(&hash[0])), uintptr(unsafe.Pointer(&size))); r == 0 {
		t.Fatal("读取证书 SHA-1 属性失败")
	}
	return hash[:size]
}

// TestSystemStoreStatusFindsInstalledCertificate 是正向对照：用存储里真实存在的证书
// 指纹查询必须命中本机作用域。没有这条测试，“永远返回未安装”也能通过反向用例。
func TestSystemStoreStatusFindsInstalledCertificate(t *testing.T) {
	hash := firstMachineRootHash(t)
	status, err := systemStoreStatus(hash)
	if err != nil {
		t.Fatalf("systemStoreStatus: %v", err)
	}
	if !status.Installed || status.Scope != "system" {
		t.Fatalf("status = %+v，期望命中本机 ROOT 存储", status)
	}
}

// TestSystemStoreStatusUnknownHash 用真实 CryptoAPI 走一遍根存储查询：随机指纹必须
// 判定为未安装，且不产生错误（也就不会拉起 certutil 子进程）。
func TestSystemStoreStatusUnknownHash(t *testing.T) {
	hash := bytes.Repeat([]byte{0x42}, 20)
	status, err := systemStoreStatus(hash)
	if err != nil {
		t.Fatalf("systemStoreStatus: %v", err)
	}
	if status.Installed {
		t.Fatalf("status = %+v，随机指纹不应命中根存储", status)
	}
}

func TestSystemStoreStatusRejectsEmptyHash(t *testing.T) {
	if _, err := systemStoreStatus(nil); err == nil {
		t.Fatal("空指纹应报错而不是返回未安装")
	}
}
