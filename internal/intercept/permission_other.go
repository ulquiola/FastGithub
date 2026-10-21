//go:build !windows

package intercept

import (
	"errors"
	"os"
	"syscall"
)

// IsPermissionError 判断错误是否由权限不足引起，典型场景是未以 root 运行。
func IsPermissionError(err error) bool {
	return errors.Is(err, os.ErrPermission) ||
		errors.Is(err, syscall.EACCES) ||
		errors.Is(err, syscall.EPERM)
}
