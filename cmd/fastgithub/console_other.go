//go:build !windows

package main

// detachConsole 在非 Windows 平台没有控制台窗口可分离。
func detachConsole() {}
