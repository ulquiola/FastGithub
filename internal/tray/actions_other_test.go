//go:build !windows

package tray

import "testing"

// 非 Windows 的导出动作是 stub：返回明确错误而不是静默成功或 panic。
func TestExportedActionsStub(t *testing.T) {
	if err := OpenURL(""); err == nil {
		t.Fatal("空 URL 应返回错误")
	}
	// 剪贴板在非 Windows 平台靠外部命令，是否可用取决于环境，只要求不 panic。
	_ = CopyToClipboard("127.0.0.1:38457")
	if err := RunElevated("install-cert"); err == nil {
		t.Fatal("非 Windows 平台没有自动提权机制，RunElevated 应返回错误")
	}
}
