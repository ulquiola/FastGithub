//go:build !windows

package dashboard

import "testing"

// 非 Windows 的默认动作必须是“尽力而为/明确提示”，不能 panic。
func TestStubActions(t *testing.T) {
	if err := openUI(""); err == nil {
		t.Fatal("UI 地址为空时 openUI 应返回错误")
	}
	// 是否装有 xdg-open 取决于环境，这里只要求不 panic/不误报成空成功。
	_ = openUI("127.0.0.1:38470")

	// 剪贴板依赖外部命令，是否成功取决于环境，只要求不 panic。
	_ = copyProxy("127.0.0.1:38457")

	if err := installCert(); err == nil {
		t.Fatal("非 Windows 平台无法自动提权，installCert 应返回提示错误")
	}
}
