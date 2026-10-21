//go:build !windows

package instance

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAcquireRejectsSecondInstance(t *testing.T) {
	old := lockFileName
	lockFileName = "test-fastgithub.lock"
	t.Cleanup(func() { lockFileName = old })

	dir := t.TempDir()
	first, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(dir); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("第二次获取锁的 err = %v，期望 ErrAlreadyRunning", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire(dir)
	if err != nil {
		t.Fatalf("释放后应能重新获取: %v", err)
	}
	if err := second.Release(); err != nil {
		t.Fatal(err)
	}
	if err := second.Release(); err != nil {
		t.Fatalf("重复释放应返回 nil: %v", err)
	}
}

func TestLockFileContainsPID(t *testing.T) {
	old := lockFileName
	lockFileName = "test-fastgithub.lock"
	t.Cleanup(func() { lockFileName = old })

	dir := t.TempDir()
	lock, err := Acquire(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	path := filepath.Join(dir, lockFileName)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("锁文件权限 = %v", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), strconv.Itoa(os.Getpid())) {
		t.Fatalf("锁文件内容 = %q，应包含 PID %d", data, os.Getpid())
	}
}
