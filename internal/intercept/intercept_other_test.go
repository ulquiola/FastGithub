//go:build !windows

package intercept

import (
	"context"
	"errors"
	"testing"
)

func TestStartUnsupported(t *testing.T) {
	if Available() {
		t.Fatal("非 Windows 不应报告可用")
	}
	if _, err := Start(context.Background(), Config{}); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("err = %v，期望 ErrUnsupported", err)
	}
}
