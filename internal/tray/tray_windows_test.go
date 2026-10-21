//go:build windows

package tray

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestMessageWindowLifecycle 真正创建隐藏消息窗口并跑一遍消息循环：
// 验证 RegisterClassExW/CreateWindowExW/NewCallback/WndProc/DestroyWindow 的
// 参数与结构体布局。通知区域图标本身需要 explorer.exe，无法在无桌面会话里断言。
func TestMessageWindowLifecycle(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		hwnd, err := createMessageWindow(0)
		if err != nil {
			done <- err
			return
		}
		activeWindow = &trayWindow{hwnd: hwnd}
		defer func() { activeWindow = nil }()

		// 先投递 WM_CLOSE，消息循环取到后应走清理并 PostQuitMessage。
		procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)
		done <- runMessageLoop()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("消息循环失败: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("消息循环未在 10 秒内退出")
	}
}

// 单实例互斥体：同一命名第二次创建必须报告 already。
func TestInstanceMutex(t *testing.T) {
	name := `Local\FastGithubTrayTestMutex`
	first, already, err := createInstanceMutex(name)
	if err != nil || already {
		t.Fatalf("首次创建互斥体失败: already=%v err=%v", already, err)
	}
	if first == 0 {
		t.Fatal("首次创建的互斥体句柄为空")
	}
	defer procCloseHandle.Call(uintptr(first))

	second, already, err := createInstanceMutex(name)
	if err != nil {
		t.Fatalf("第二次创建互斥体失败: %v", err)
	}
	if !already {
		procCloseHandle.Call(uintptr(second))
		t.Fatal("同名互斥体第二次创建应报告 already")
	}
	if second != 0 {
		t.Fatalf("已有实例时不应返回句柄: %v", second)
	}
}

// TestAcquireSingleInstance 验证 CLI 启动前使用的单实例锁：第二次获取命中
// ErrAlreadyRunning，释放后可以重新获取。互斥体名换成测试专用，避免与真实实例冲突。
func TestAcquireSingleInstance(t *testing.T) {
	oldName, oldFallback := instanceMutexName, instanceFallbackMutexName
	instanceMutexName = `Local\FastGithubTrayAcquireTest`
	instanceFallbackMutexName = `Local\FastGithubTrayAcquireTestFallback`
	t.Cleanup(func() {
		instanceMutexName = oldName
		instanceFallbackMutexName = oldFallback
	})

	release, err := AcquireSingleInstance()
	if err != nil {
		t.Fatalf("首次获取单实例锁失败: %v", err)
	}
	if _, err := AcquireSingleInstance(); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("第二次获取 = %v，期望 ErrAlreadyRunning", err)
	}
	release()
	release, err = AcquireSingleInstance()
	if err != nil {
		t.Fatalf("释放后重新获取失败: %v", err)
	}
	release()
}

// TestRunCleanupOnContextCancel 调用真实的 Run：通知区域调用替换为成功且记录
// NIM_DELETE 的假实现，因此没有 explorer.exe 也能完整走一遍 ctx 取消 → WM_CLOSE
// → 删除图标 → 销毁窗口 → 消息循环退出。
func TestRunCleanupOnContextCancel(t *testing.T) {
	oldNotify := shellNotifyIcon
	added := make(chan syscall.Handle, 1)
	deleted := make(chan struct{}, 4)
	shellNotifyIcon = func(message uint32, nid *notifyIconData) (bool, error) {
		switch message {
		case nimAdd:
			added <- nid.HWnd
		case nimDelete:
			select {
			case deleted <- struct{}{}:
			default:
			}
		}
		return true, nil
	}
	t.Cleanup(func() { shellNotifyIcon = oldNotify })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Run(ctx, Options{Version: "vtest", ProxyAddr: "127.0.0.1:38457"})
	}()

	var hwnd syscall.Handle
	select {
	case hwnd = <-added:
	case <-time.After(10 * time.Second):
		t.Fatal("Run 未在 10 秒内添加图标")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ctx 取消后 Run 返回错误: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ctx 取消后 Run 未在 10 秒内退出")
	}

	select {
	case <-deleted:
	default:
		t.Fatal("清理路径没有调用 NIM_DELETE")
	}
	if hwnd == 0 {
		t.Fatal("NIM_ADD 的窗口句柄为空")
	}
	if r, _, _ := procIsWindow.Call(uintptr(hwnd)); r != 0 {
		t.Fatalf("Run 返回后消息窗口 %v 仍然存在", hwnd)
	}
}

// Explorer 重启后会广播注册的 TaskbarCreated 消息；WndProc 必须重新添加图标。
func TestTaskbarCreatedReaddsIcon(t *testing.T) {
	if taskbarCreatedMessage == 0 {
		t.Fatal("RegisterWindowMessageW(TaskbarCreated) 返回 0")
	}

	oldNotify := shellNotifyIcon
	var messages []uint32
	var tips []string
	shellNotifyIcon = func(message uint32, nid *notifyIconData) (bool, error) {
		messages = append(messages, message)
		if message == nimAdd {
			tips = append(tips, syscall.UTF16ToString(nid.SzTip[:]))
		}
		return true, nil
	}
	t.Cleanup(func() { shellNotifyIcon = oldNotify })

	oldWindow := activeWindow
	activeWindow = &trayWindow{
		hwnd: 42,
		opts: Options{Version: "vtest", ProxyAddr: "127.0.0.1:38457"},
	}
	t.Cleanup(func() { activeWindow = oldWindow })

	if r := wndProc(42, taskbarCreatedMessage, 0, 0); r != 0 {
		t.Fatalf("WndProc(TaskbarCreated) = %d，期望 0", r)
	}
	if len(messages) == 0 || messages[0] != nimAdd {
		t.Fatalf("TaskbarCreated 后应调用 NIM_ADD，实际 %v", messages)
	}
	if len(tips) == 0 || !strings.Contains(tips[0], "vtest") {
		t.Fatalf("重新添加的提示文本应包含版本号，实际 %v", tips)
	}
}

// installCertElevated 只把 ERROR_CANCELLED(1223) 当成无操作，其余 ShellExecuteW
// 失败必须返回给菜单层提示。
func TestInstallCertElevated(t *testing.T) {
	old := shellExecute
	t.Cleanup(func() { shellExecute = old })

	cases := []struct {
		name    string
		result  error
		wantErr bool
	}{
		{"成功", nil, false},
		{"UAC 取消（返回码 1223）", &shellExecuteError{op: "runas", code: 1223}, false},
		{"UAC 取消（last error 1223）", &shellExecuteError{op: "runas", code: 5, errno: errorCancelled}, false},
		{"真实失败", &shellExecuteError{op: "runas", code: 5, errno: syscall.Errno(5)}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shellExecute = func(_ syscall.Handle, op, file, params string, _ int32) error {
				if op != "runas" || params != "install-cert" || file == "" {
					t.Fatalf("ShellExecuteW 参数 = op=%q file=%q params=%q", op, file, params)
				}
				return tc.result
			}
			err := installCertElevated(0)
			if tc.wantErr != (err != nil) {
				t.Fatalf("installCertElevated 错误 = %v，wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
