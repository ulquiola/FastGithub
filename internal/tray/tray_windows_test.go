//go:build windows

package tray

import (
	"runtime"
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
