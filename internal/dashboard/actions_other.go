//go:build !windows

package dashboard

import (
	"errors"

	"fastgithub-go/internal/tray"
)

// openUI 用系统默认浏览器（xdg-open/open）打开面板地址。
func openUI(addr string) error {
	if addr == "" {
		return errors.New("UI 地址为空")
	}
	return tray.OpenURL(uiURL(addr))
}

// copyProxy 尽力把代理地址写入系统剪贴板；没有桌面会话时会失败。
func copyProxy(addr string) error {
	if addr == "" {
		return errors.New("代理地址为空")
	}
	return tray.CopyToClipboard(addr)
}

// installCert 在非 Windows 平台没有自动提权：提示用户以 root 手动安装。
func installCert() error {
	if err := tray.RunElevated("install-cert"); err != nil {
		return errors.New("非 Windows 平台请手动安装证书：以 root 运行 fastgithub install-cert")
	}
	return nil
}
