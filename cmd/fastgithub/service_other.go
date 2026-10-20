//go:build !windows

package main

// runAsService 在非 Windows 平台始终按交互模式运行。
func runAsService() (bool, int) { return false, 0 }
