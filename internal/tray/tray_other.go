//go:build !windows

package tray

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// Run 在非 Windows 平台没有通知区域实现。
func Run(_ context.Context, _ Options) error {
	return ErrUnsupported
}

// IsAdmin 在非 Windows 平台恒为 false。
func IsAdmin() bool {
	return false
}

// AcquireSingleInstance 在非 Windows 平台没有托盘，无需单实例锁；
// 返回空释放函数，让 CLI 仍能走“托盘不可用则回退控制台”的路径。
func AcquireSingleInstance() (func(), error) {
	return func() {}, nil
}

// CopyToClipboard 在非 Windows 平台尽力调用系统剪贴板命令。
//
// 没有桌面会话或缺少外部命令时返回错误，调用方按“提示即可”处理。
func CopyToClipboard(text string) error {
	cmd, err := clipboardCommand()
	if err != nil {
		return err
	}
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("复制到剪贴板失败: %w", err)
	}
	return nil
}

// clipboardCommand 按平台挑一个可用的剪贴板命令。
func clipboardCommand() (*exec.Cmd, error) {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"pbcopy"}}
	case "linux", "freebsd", "openbsd", "netbsd":
		candidates = [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
		}
	default:
		return nil, ErrUnsupported
	}
	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate[0]); err == nil {
			return exec.Command(path, candidate[1:]...), nil
		}
	}
	return nil, errors.New("未找到可用的剪贴板命令（pbcopy/xclip/xsel/wl-copy）")
}

// OpenURL 用系统默认浏览器打开 url（Linux 用 xdg-open，macOS 用 open）。
func OpenURL(url string) error {
	if url == "" {
		return errors.New("URL 为空")
	}
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return fmt.Errorf("找不到打开界面的命令 %s: %w", name, err)
	}
	cmd := exec.Command(path, url)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("调用 %s 打开界面失败: %w", name, err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// RunElevated 在非 Windows 平台没有自动提权机制。
func RunElevated(_ string) error {
	return errors.New("当前平台不支持自动提权，请以 root 运行 fastgithub install-cert")
}
