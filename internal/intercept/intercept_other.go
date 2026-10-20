//go:build !windows

package intercept

import "context"

// Available 在非 Windows 平台返回 false。
func Available() bool { return false }

// Start 在非 Windows 平台返回 ErrUnsupported。
func Start(ctx context.Context, cfg Config) (*Handle, error) {
	return nil, ErrUnsupported
}
