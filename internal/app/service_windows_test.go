//go:build windows

package app

import (
	"errors"
	"fmt"
	"syscall"
	"testing"
)

func TestIsServiceNotExist(t *testing.T) {
	wrapped := fmt.Errorf("OpenService: %w", errorServiceNotExist)
	if !isServiceNotExist(wrapped) {
		t.Fatal("包裹后的 1060 应判定为不存在")
	}
	if isServiceNotExist(errors.New("boom")) {
		t.Fatal("其它错误不应判定为不存在")
	}
	if isServiceNotExist(fmt.Errorf("OpenService: %w", syscall.ERROR_ACCESS_DENIED)) {
		t.Fatal("拒绝访问不应判定为不存在")
	}
}
