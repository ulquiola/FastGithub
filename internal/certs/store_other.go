//go:build !windows

package certs

import "errors"

// systemStoreStatus 只在 Windows 使用；其它平台走各自的信任库实现。
func systemStoreStatus([]byte) (TrustStatus, error) {
	return TrustStatus{}, errors.New("系统证书存储探测仅在 Windows 上可用")
}
