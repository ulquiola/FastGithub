//go:build windows

package tray

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var procRegDeleteKeyW = advapi32.NewProc("RegDeleteKeyW")

// TestSetAutostartRoundTrip 用临时 HKCU 子键做真实的注册表往返，验证写入的
// 命令行带 --path 绝对路径；绝不触碰真实的 Run 项，测试结束删除临时键。
func TestSetAutostartRoundTrip(t *testing.T) {
	oldKey := autostartKey
	autostartKey = fmt.Sprintf(`Software\FastGithubTrayTest_%d`, time.Now().UnixNano())
	t.Cleanup(func() {
		subKey, err := syscall.UTF16PtrFromString(autostartKey)
		if err == nil {
			procRegDeleteKeyW.Call(uintptr(syscall.HKEY_CURRENT_USER), uintptr(unsafe.Pointer(subKey)))
		}
		autostartKey = oldKey
	})
	if !strings.HasPrefix(autostartKey, `Software\FastGithubTrayTest_`) {
		t.Fatalf("测试键必须在临时子键下，实际 %q", autostartKey)
	}

	if err := SetAutostart(true, ""); err == nil {
		t.Fatal("缺少配置路径时 SetAutostart 应报错，而不是写入无法工作的命令")
	}

	configPath := filepath.Join(t.TempDir(), "配置 目录", "fastgithub.toml")
	if err := SetAutostart(true, configPath); err != nil {
		// DSH 等受限令牌会拒绝一切 HKCU 写入；这不是产品缺陷，跳过真实注册表段。
		if errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
			t.Skipf("当前会话不允许写 HKCU，跳过注册表往返：%v", err)
		}
		t.Fatalf("SetAutostart(true): %v", err)
	}
	if enabled, err := AutostartEnabled(); err != nil || !enabled {
		t.Fatalf("AutostartEnabled = %v, %v，期望 true, nil", enabled, err)
	}

	got := readAutostartValue(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	if want := autostartCommand(exe, configPath); got != want {
		t.Fatalf("注册表值 = %q，期望 %q", got, want)
	}
	if !strings.Contains(got, `--path "`+configPath+`"`) {
		t.Fatalf("注册表值缺少带引号的 --path：%q", got)
	}

	if err := SetAutostart(false, ""); err != nil {
		t.Fatalf("SetAutostart(false): %v", err)
	}
	if enabled, err := AutostartEnabled(); err != nil || enabled {
		t.Fatalf("删除后 AutostartEnabled = %v, %v，期望 false, nil", enabled, err)
	}
}

func readAutostartValue(t *testing.T) string {
	t.Helper()
	key, err := openAutostartKey(syscall.KEY_QUERY_VALUE)
	if err != nil {
		t.Fatalf("打开测试键失败: %v", err)
	}
	if key == 0 {
		t.Fatal("测试键不存在")
	}
	defer procRegCloseKey.Call(uintptr(key))

	name, err := syscall.UTF16PtrFromString(autostartValue)
	if err != nil {
		t.Fatalf("UTF16PtrFromString: %v", err)
	}
	var valType, size uint32
	r, _, _ := procRegQueryValueExW.Call(
		uintptr(key),
		uintptr(unsafe.Pointer(name)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		0,
		uintptr(unsafe.Pointer(&size)),
	)
	if errno := syscall.Errno(r); errno != 0 && errno != errorMoreData {
		t.Fatalf("查询值长度失败: %v", errno)
	}
	if size == 0 {
		t.Fatal("注册表值长度为 0")
	}
	buf := make([]uint16, size/2+1)
	r, _, _ = procRegQueryValueExW.Call(
		uintptr(key),
		uintptr(unsafe.Pointer(name)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if errno := syscall.Errno(r); errno != 0 {
		t.Fatalf("读取注册表值失败: %v", errno)
	}
	if valType != syscall.REG_SZ {
		t.Fatalf("值类型 = %d，期望 REG_SZ", valType)
	}
	return syscall.UTF16ToString(buf)
}
