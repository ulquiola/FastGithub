//go:build windows

package instance

import (
	"errors"
	"testing"
)

func TestAcquireRejectsSecondInstance(t *testing.T) {
	oldName, oldFallback := lockName, lockFallbackName
	lockName = `Global\FastGithubProxyTest`
	lockFallbackName = `Local\FastGithubProxyTest`
	t.Cleanup(func() { lockName, lockFallbackName = oldName, oldFallback })

	first, err := Acquire("")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Acquire(""); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("第二次获取锁的 err = %v，期望 ErrAlreadyRunning", err)
	}
	if err := first.Release(); err != nil {
		t.Fatal(err)
	}
	second, err := Acquire("")
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
