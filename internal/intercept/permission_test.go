package intercept

import (
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestIsPermissionError(t *testing.T) {
	if IsPermissionError(nil) {
		t.Fatal("nil 不应判定为权限错误")
	}
	if IsPermissionError(errors.New("boom")) {
		t.Fatal("普通错误不应判定为权限错误")
	}
	if !IsPermissionError(fmt.Errorf("wrap: %w", os.ErrPermission)) {
		t.Fatal("包裹后的 os.ErrPermission 应判定为权限错误")
	}
}
