//go:build windows

package tray

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	// autostartValue 是写入 Run 键的值名。
	autostartValue = "FastGithub"

	errorFileNotFound = syscall.Errno(2)
	errorMoreData     = syscall.Errno(234)
)

// autostartKey 是当前用户的登录启动项所在位置，不需要管理员权限。
// 变量而非常量：测试会指向临时 HKCU 子键，避免触碰真实的 Run 项。
var autostartKey = `Software\Microsoft\Windows\CurrentVersion\Run`

var (
	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegCreateKeyExW  = advapi32.NewProc("RegCreateKeyExW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")
)

// AutostartEnabled 报告 HKCU\...\Run 下是否已有 FastGithub 值。
func AutostartEnabled() (bool, error) {
	key, err := openAutostartKey(syscall.KEY_QUERY_VALUE)
	if err != nil {
		return false, err
	}
	if key == 0 {
		return false, nil
	}
	defer procRegCloseKey.Call(uintptr(key))

	name, err := syscall.UTF16PtrFromString(autostartValue)
	if err != nil {
		return false, err
	}
	var valType uint32
	var size uint32
	r, _, _ := procRegQueryValueExW.Call(
		uintptr(key),
		uintptr(unsafe.Pointer(name)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		0,
		uintptr(unsafe.Pointer(&size)),
	)
	errno := syscall.Errno(r)
	if errno == errorFileNotFound {
		return false, nil
	}
	// 传 nil 缓冲区时可能返回 ERROR_MORE_DATA 并给出所需长度，同样说明值存在。
	if errno != 0 && errno != errorMoreData {
		return false, fmt.Errorf("查询开机启动项失败: %w", errno)
	}
	if size == 0 || (valType != syscall.REG_SZ && valType != syscall.REG_EXPAND_SZ) {
		return false, nil
	}
	return true, nil
}

// SetAutostart 写入或删除当前用户的开机启动项。
// configPath 是主配置文件的绝对路径，会以 --path 写入命令行。
func SetAutostart(enabled bool, configPath string) error {
	if !enabled {
		return disableAutostart()
	}
	if configPath == "" {
		return fmt.Errorf("缺少主配置文件路径，无法写入开机启动项")
	}
	// Run 键启动时没有工作目录，必须写入绝对路径，否则会退回“找不到配置”的旧问题。
	if !filepath.IsAbs(configPath) {
		return fmt.Errorf("主配置文件路径必须是绝对路径: %s", configPath)
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位可执行文件失败: %w", err)
	}
	key, err := createAutostartKey()
	if err != nil {
		return err
	}
	if key == 0 {
		return nil
	}
	defer procRegCloseKey.Call(uintptr(key))

	name, err := syscall.UTF16PtrFromString(autostartValue)
	if err != nil {
		return err
	}
	command, err := syscall.UTF16FromString(autostartCommand(exe, configPath))
	if err != nil {
		return err
	}
	r, _, _ := procRegSetValueExW.Call(
		uintptr(key),
		uintptr(unsafe.Pointer(name)),
		0,
		uintptr(syscall.REG_SZ),
		uintptr(unsafe.Pointer(&command[0])),
		uintptr(len(command)*2),
	)
	if errno := syscall.Errno(r); errno != 0 {
		return fmt.Errorf("写入开机启动项失败: %w", errno)
	}
	return nil
}

func disableAutostart() error {
	key, err := openAutostartKey(syscall.KEY_SET_VALUE)
	if err != nil {
		return err
	}
	if key == 0 {
		return nil
	}
	defer procRegCloseKey.Call(uintptr(key))

	name, err := syscall.UTF16PtrFromString(autostartValue)
	if err != nil {
		return err
	}
	r, _, _ := procRegDeleteValueW.Call(uintptr(key), uintptr(unsafe.Pointer(name)))
	if errno := syscall.Errno(r); errno != 0 && errno != errorFileNotFound {
		return fmt.Errorf("删除开机启动项失败: %w", errno)
	}
	return nil
}

// openAutostartKey 打开 Run 键；键不存在时返回 (0, nil)，表示“未启用”而不是错误。
func openAutostartKey(access uint32) (syscall.Handle, error) {
	subKey, err := syscall.UTF16PtrFromString(autostartKey)
	if err != nil {
		return 0, err
	}
	var key syscall.Handle
	r, _, _ := procRegOpenKeyExW.Call(
		uintptr(syscall.HKEY_CURRENT_USER),
		uintptr(unsafe.Pointer(subKey)),
		0,
		uintptr(access),
		uintptr(unsafe.Pointer(&key)),
	)
	switch errno := syscall.Errno(r); errno {
	case 0:
		return key, nil
	case errorFileNotFound:
		return 0, nil
	default:
		return 0, fmt.Errorf("打开开机启动注册表项失败: %w", errno)
	}
}

func createAutostartKey() (syscall.Handle, error) {
	subKey, err := syscall.UTF16PtrFromString(autostartKey)
	if err != nil {
		return 0, err
	}
	var key syscall.Handle
	var disposition uint32
	r, _, _ := procRegCreateKeyExW.Call(
		uintptr(syscall.HKEY_CURRENT_USER),
		uintptr(unsafe.Pointer(subKey)),
		0,
		0,
		0,
		uintptr(syscall.KEY_SET_VALUE|syscall.KEY_QUERY_VALUE),
		0,
		uintptr(unsafe.Pointer(&key)),
		uintptr(unsafe.Pointer(&disposition)),
	)
	if errno := syscall.Errno(r); errno != 0 {
		return 0, fmt.Errorf("创建开机启动注册表项失败: %w", errno)
	}
	return key, nil
}
