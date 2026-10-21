//go:build windows

package instance

import (
	"errors"
	"fmt"
	"sync"
	"syscall"
	"unsafe"
)

// 锁名可在测试中替换，避免与真实运行实例互相影响。
var (
	lockName         = `Global\FastGithubProxy`
	lockFallbackName = `Local\FastGithubProxy`
)

const errorAlreadyExists = syscall.Errno(183)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW = kernel32.NewProc("CreateMutexW")
	procCloseHandle  = kernel32.NewProc("CloseHandle")
)

type mutexLock struct {
	handle syscall.Handle
	once   sync.Once
}

func acquire(dir string) (func() error, error) {
	// Global\ 可跨会话（服务 vs 手工 run）；普通用户没有权限时退回当前会话。
	handle, err := createMutex(lockName)
	if err != nil && !errors.Is(err, ErrAlreadyRunning) {
		handle, err = createMutex(lockFallbackName)
	}
	if err != nil {
		return nil, err
	}
	lock := &mutexLock{handle: handle}
	return func() error {
		lock.once.Do(func() { procCloseHandle.Call(uintptr(lock.handle)) })
		return nil
	}, nil
}

func createMutex(name string) (syscall.Handle, error) {
	ptr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, err
	}
	handle, _, errno := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(ptr)))
	if errno == errorAlreadyExists {
		if handle != 0 {
			procCloseHandle.Call(handle)
		}
		return 0, ErrAlreadyRunning
	}
	if handle == 0 {
		return 0, fmt.Errorf("创建单实例互斥体 %s 失败: %w", name, errno)
	}
	return syscall.Handle(handle), nil
}
