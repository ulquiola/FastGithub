// Package instance 提供进程级单实例锁：同一份配置目录只允许一个代理进程。
package instance

import "errors"

// ErrAlreadyRunning 表示已有实例持有单实例锁。
var ErrAlreadyRunning = errors.New("已有 FastGithub 实例在运行")

// Lock 是已持有的单实例锁；Release 可安全重复调用。
type Lock struct {
	release func() error
}

// Acquire 在 dir 下获取单实例锁；dir 为空时使用当前目录。
func Acquire(dir string) (*Lock, error) {
	release, err := acquire(dir)
	if err != nil {
		return nil, err
	}
	return &Lock{release: release}, nil
}

// Release 释放锁；重复调用返回 nil。
func (l *Lock) Release() error {
	if l == nil || l.release == nil {
		return nil
	}
	release := l.release
	l.release = nil
	return release()
}
