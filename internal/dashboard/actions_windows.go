//go:build windows

package dashboard

import (
	"errors"

	"fastgithub-go/internal/tray"
)

// openUI 用系统默认浏览器打开面板地址。
func openUI(addr string) error {
	if addr == "" {
		return errors.New("UI 地址为空")
	}
	return tray.OpenURL(uiURL(addr))
}

// copyProxy 把代理地址写入系统剪贴板。
func copyProxy(addr string) error {
	if addr == "" {
		return errors.New("代理地址为空")
	}
	return tray.CopyToClipboard(addr)
}

// installCert 以管理员身份重新运行 install-cert（会弹 UAC，用户取消视为无操作）。
func installCert() error {
	return tray.RunElevated("install-cert")
}
