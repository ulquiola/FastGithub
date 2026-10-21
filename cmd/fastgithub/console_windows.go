//go:build windows

package main

import "syscall"

var procFreeConsole = syscall.NewLazyDLL("kernel32.dll").NewProc("FreeConsole")

// detachConsole 把当前进程从控制台分离，双击启动时的黑色窗口会随之关闭。
// 仍可用 fastgithub tray --console 保留窗口查看日志。
func detachConsole() {
	procFreeConsole.Call()
}
