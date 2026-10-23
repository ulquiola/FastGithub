//go:build windows

package procattr

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// TestHideSuppressesConsoleWindow 锁定托盘模式的回归：进程已 FreeConsole 时，
// 任何控制台子进程（certutil、sc 等）都会新建一个可见的控制台窗口，UI 每 5 秒
// 轮询证书状态就会闪一次黑框。Hide 必须同时设置 HideWindow 与 CREATE_NO_WINDOW。
func TestHideSuppressesConsoleWindow(t *testing.T) {
	cmd := exec.Command("certutil", "-store", "Root")
	Hide(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr 为 nil，子进程会弹出控制台窗口")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Error("HideWindow = false，期望 true")
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Errorf("CreationFlags = %#x，缺少 CREATE_NO_WINDOW(%#x)", cmd.SysProcAttr.CreationFlags, createNoWindow)
	}
}

// TestHideKeepsExistingFlags 保证 Hide 不丢弃调用方已经设置的进程属性。
func TestHideKeepsExistingFlags(t *testing.T) {
	cmd := exec.Command("certutil")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
	Hide(cmd)
	if cmd.SysProcAttr.CreationFlags&syscall.CREATE_NEW_PROCESS_GROUP == 0 {
		t.Errorf("已有的 CREATE_NEW_PROCESS_GROUP 被覆盖: %#x", cmd.SysProcAttr.CreationFlags)
	}
	if cmd.SysProcAttr.CreationFlags&createNoWindow == 0 {
		t.Errorf("CreationFlags = %#x，缺少 CREATE_NO_WINDOW", cmd.SysProcAttr.CreationFlags)
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Error("HideWindow = false，期望 true")
	}
}

// TestHiddenChildStillCapturesOutput 确认隐藏窗口不会影响管道：CREATE_NO_WINDOW
// 的子进程仍然能被 CombinedOutput 正常捕获，否则 certs/sysproxy 会拿不到 certutil
// 的输出去判断“证书是否已安装”。
func TestHiddenChildStillCapturesOutput(t *testing.T) {
	if os.Getenv("PROCATTR_CHILD") == "1" {
		os.Stdout.WriteString("child-output")
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestHiddenChildStillCapturesOutput")
	cmd.Env = append(os.Environ(), "PROCATTR_CHILD=1")
	Hide(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("隐藏窗口的子进程执行失败: %v\n%s", err, out)
	}
	if string(out) != "child-output" {
		t.Fatalf("子进程输出 = %q，期望 %q", out, "child-output")
	}
}
