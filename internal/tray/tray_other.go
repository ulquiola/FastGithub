//go:build !windows

package tray

import "context"

// Run 在非 Windows 平台没有通知区域实现。
func Run(_ context.Context, _ Options) error {
	return ErrUnsupported
}

// IsAdmin 在非 Windows 平台恒为 false。
func IsAdmin() bool {
	return false
}
