//go:build !windows

package tray

// AutostartEnabled 在非 Windows 平台没有注册表启动项。
func AutostartEnabled() (bool, error) {
	return false, ErrUnsupported
}

// SetAutostart 在非 Windows 平台没有注册表启动项。
func SetAutostart(_ bool) error {
	return ErrUnsupported
}
