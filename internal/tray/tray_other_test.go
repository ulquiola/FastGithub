//go:build !windows

package tray

import (
	"context"
	"errors"
	"testing"
)

func TestStub(t *testing.T) {
	if err := Run(context.Background(), Options{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Run 错误 = %v，期望 ErrUnsupported", err)
	}
	if enabled, err := AutostartEnabled(); !errors.Is(err, ErrUnsupported) || enabled {
		t.Fatalf("AutostartEnabled = %v, %v，期望 false, ErrUnsupported", enabled, err)
	}
	if err := SetAutostart(true, "/tmp/fastgithub.toml"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("SetAutostart 错误 = %v，期望 ErrUnsupported", err)
	}
	release, err := AcquireSingleInstance()
	if err != nil || release == nil {
		t.Fatalf("AcquireSingleInstance: releaseNil=%v err=%v，期望可用空释放函数", release == nil, err)
	}
	release()
	if IsAdmin() {
		t.Fatal("非 Windows 平台 IsAdmin 应为 false")
	}
}
