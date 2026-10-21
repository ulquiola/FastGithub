//go:build !windows

package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// lockFileName 可在测试中替换，避免与真实运行实例互相影响。
var lockFileName = "fastgithub.lock"

func acquire(dir string) (func() error, error) {
	if dir == "" {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("创建锁目录 %s 失败: %w", dir, err)
	}
	path := filepath.Join(dir, lockFileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("打开单实例锁 %s 失败: %w", path, err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("获取单实例锁 %s 失败: %w", path, err)
	}
	// 写入 PID 便于排查；写失败不影响锁语义。
	_ = file.Truncate(0)
	_, _ = file.Seek(0, 0)
	fmt.Fprintf(file, "%d\n", os.Getpid())

	release := func() error {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		return file.Close()
	}
	return release, nil
}
