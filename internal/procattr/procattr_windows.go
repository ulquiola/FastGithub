//go:build windows

// Package procattr 统一设置子进程的启动属性。
package procattr

import (
	"os/exec"
	"syscall"
)

// createNoWindow 对应 CreateProcess 的 CREATE_NO_WINDOW 标志。
// Go 的 syscall 包没有导出它，只能自己定义。
const createNoWindow = 0x08000000

// Hide 让 cmd 启动的子进程不创建可见的控制台窗口。
//
// 托盘模式启动后会 FreeConsole()，此后所有控制台子进程（certutil、sc 等）都会
// 新建自己的控制台窗口；UI 每 5 秒轮询 /api/cert/status 就会闪一次黑框。
// HideWindow 只设置 STARTF_USESHOWWINDOW/SW_HIDE，窗口仍会被创建再隐藏，
// 因此同时设置 CREATE_NO_WINDOW 才能让子进程根本不出现窗口。
func Hide(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
