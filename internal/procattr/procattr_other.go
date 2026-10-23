//go:build !windows

// Package procattr 统一设置子进程的启动属性。
package procattr

import "os/exec"

// Hide 在非 Windows 平台没有控制台窗口需要隐藏，保持空实现以便调用点无需分平台。
func Hide(*exec.Cmd) {}
