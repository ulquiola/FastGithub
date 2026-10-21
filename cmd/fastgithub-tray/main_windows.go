//go:build windows

// fastgithub-tray 是 GUI 子系统的启动器：双击它不会出现控制台黑框，
// 它只负责以管理员身份静默拉起同目录的 fastgithub.exe tray。
package main

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

var (
	shell32           = syscall.NewLazyDLL("shell32.dll")
	procShellExecuteW = shell32.NewProc("ShellExecuteW")
)

func utf16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return nil
	}
	return p
}

func main() {
	exe, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}
	dir := filepath.Dir(exe)
	target := filepath.Join(dir, "fastgithub.exe")

	params := "tray"
	if cfg := filepath.Join(dir, "fastgithub.toml"); fileExists(cfg) {
		params = `tray --path "` + cfg + `"`
	}

	const swHide = 0
	r, _, _ := procShellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(utf16("runas"))),
		uintptr(unsafe.Pointer(utf16(target))),
		uintptr(unsafe.Pointer(utf16(params))),
		uintptr(unsafe.Pointer(utf16(dir))),
		swHide,
	)
	// ShellExecuteW 返回值 <= 32 表示失败（用户取消 UAC 时通常为 5）。
	if r <= 32 {
		os.Exit(1)
	}
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
