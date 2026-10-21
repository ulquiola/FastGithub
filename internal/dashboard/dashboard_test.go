package dashboard

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestStatusText(t *testing.T) {
	o := Options{
		Version:   "v9.8.7",
		ProxyAddr: "127.0.0.1:38457",
		UIAddr:    "127.0.0.1:38470",
		Intercept: "未启用（需要管理员）",
	}
	got := StatusText(o, 3*time.Minute+12*time.Second)
	for _, want := range []string{
		"v9.8.7", "127.0.0.1:38457", "http://127.0.0.1:38470/",
		"未启用（需要管理员）", "00:03:12",
		"[1]", "[2]", "[3]", "[q]",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("状态文本应包含 %q，实际 %q", want, got)
		}
	}
	if !strings.Contains(got, "\n") {
		t.Fatalf("状态文本应为多行区块，实际 %q", got)
	}

	// 空字段退回可读的默认值，避免面板出现空洞。
	got = StatusText(Options{}, 0)
	for _, want := range []string{"dev", "已关闭", "00:00:00"} {
		if !strings.Contains(got, want) {
			t.Fatalf("空 Options 的状态文本应包含 %q，实际 %q", want, got)
		}
	}

	// 已带协议与结尾斜杠的 UI 地址不能重复拼接。
	got = StatusText(Options{UIAddr: "http://127.0.0.1:38470/"}, 0)
	if strings.Contains(got, "http://http://") || strings.Contains(got, "38470//") {
		t.Fatalf("UI 地址被重复拼接：%q", got)
	}
}

func TestFormatUptime(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{0, "00:00:00"},
		{5 * time.Second, "00:00:05"},
		{59 * time.Second, "00:00:59"},
		{65 * time.Second, "00:01:05"},
		{time.Hour + 2*time.Minute + 3*time.Second, "01:02:03"},
		{100*time.Hour + 59*time.Minute + 59*time.Second, "100:59:59"},
		{-time.Second, "00:00:00"},
	}
	for _, tc := range cases {
		if got := formatUptime(tc.in); got != tc.want {
			t.Fatalf("formatUptime(%v) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestHandleKey(t *testing.T) {
	var opened, copied, installed int
	o := Options{
		OpenUI:      func() error { opened++; return nil },
		CopyProxy:   func() error { copied++; return nil },
		InstallCert: func() error { installed++; return nil },
	}

	for _, key := range []byte{'1', '2', '3'} {
		if quit := HandleKey(o, key); quit {
			t.Fatalf("按键 %q 不应退出", key)
		}
	}
	if opened != 1 || copied != 1 || installed != 1 {
		t.Fatalf("1/2/3 应各触发一次回调，实际 opened=%d copied=%d installed=%d", opened, copied, installed)
	}

	for _, key := range []byte{'q', 'Q', 0x03} {
		if !HandleKey(o, key) {
			t.Fatalf("按键 %#x 应退出", key)
		}
	}
	if opened != 1 || copied != 1 || installed != 1 {
		t.Fatalf("退出键不应触发回调，实际 opened=%d copied=%d installed=%d", opened, copied, installed)
	}

	// 未知按键是无副作用的。
	if quit := HandleKey(o, 'x'); quit {
		t.Fatal("未知按键不应退出")
	}
	if opened != 1 || copied != 1 || installed != 1 {
		t.Fatalf("未知按键不应触发回调，实际 opened=%d copied=%d installed=%d", opened, copied, installed)
	}

	// 回调返回错误只记录，不改变按键语义。
	o.OpenUI = func() error { opened++; return errForTest }
	if quit := HandleKey(o, '1'); quit {
		t.Fatal("回调报错不应退出面板")
	}
	if opened != 2 {
		t.Fatalf("opened = %d，期望 2", opened)
	}
}

var errForTest = testError("回调失败")

type testError string

func (e testError) Error() string { return string(e) }

// 回调为 nil 时（CLI 只填地址、由 Run 补默认动作之前的场景）按键必须不 panic。
func TestHandleKeyNilCallbacks(t *testing.T) {
	for _, key := range []byte{'1', '2', '3', 'x'} {
		if quit := HandleKey(Options{}, key); quit {
			t.Fatalf("按键 %q 不应退出", key)
		}
	}
	if !HandleKey(Options{}, 'q') {
		t.Fatal("q 应退出")
	}
}

// lockedBuffer 让测试可以边跑 Run 边安全读取输出。
type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// waitForOutput 等待面板写出第一段内容。
func waitForOutput(t *testing.T, out *lockedBuffer) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if s := out.String(); s != "" {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("面板在 3 秒内没有任何输出")
	return ""
}

// runAsync 在后台跑 Run，返回取消函数与结果通道。
func runAsync(t *testing.T, o Options) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, o) }()
	t.Cleanup(cancel)
	return cancel, done
}

func waitDone(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run 返回错误: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run 未在取消后及时返回")
	}
}

// waitFor 轮询等待断言成立，避免依赖固定的 sleep 时长。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待 %s 超时", what)
}

// fakeRefreshTicker 记录 Stop 是否被调用，验证 Run 不泄漏刷新定时器。
type fakeRefreshTicker struct {
	ch      chan time.Time
	stopped atomic.Bool
}

func (f *fakeRefreshTicker) C() <-chan time.Time { return f.ch }
func (f *fakeRefreshTicker) Stop()               { f.stopped.Store(true) }

// Input 是 Run 的读键测试缝：按键触发回调，q 立即返回并停止 ticker。
func TestRunInputKeyTriggersActionAndQuit(t *testing.T) {
	out := &lockedBuffer{}
	input := make(chan byte, 8)
	var copied atomic.Int32
	_, done := runAsync(t, Options{
		Version:   "v1",
		ProxyAddr: "127.0.0.1:38457",
		UIAddr:    "127.0.0.1:38470",
		Input:     input,
		Out:       out,
		CopyProxy: func() error {
			copied.Add(1)
			return nil
		},
	})
	waitForOutput(t, out)

	input <- '2'
	waitFor(t, "复制回调与反馈", func() bool {
		return copied.Load() == 1 && strings.Contains(out.String(), "已复制代理地址: 127.0.0.1:38457")
	})

	started := time.Now()
	input <- 'q'
	waitDone(t, done)
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("q 返回耗时 %v，期望 1s 内", elapsed)
	}
	got := out.String()
	if !strings.Contains(got, "正在退出 FastGithub...") {
		t.Fatalf("q 应打印退出提示，实际 %q", got)
	}
	if strings.Contains(got, "\r") {
		t.Fatalf("非 TTY 输出不应包含回车符：%q", got)
	}
}

func TestRunStopsRefreshTicker(t *testing.T) {
	old := newRefreshTicker
	t.Cleanup(func() { newRefreshTicker = old })
	fake := &fakeRefreshTicker{ch: make(chan time.Time, 1)}
	var gotInterval time.Duration
	newRefreshTicker = func(d time.Duration) refreshTicker {
		gotInterval = d
		return fake
	}

	out := &lockedBuffer{}
	input := make(chan byte, 1)
	_, done := runAsync(t, Options{Version: "v1", Input: input, Out: out})
	waitForOutput(t, out)

	// 假 ticker 触发一次重绘，覆盖 Run 的定时刷新分支。
	initialLines := strings.Count(out.String(), "\n")
	fake.ch <- time.Now()
	waitFor(t, "定时重绘", func() bool {
		return strings.Count(out.String(), "\n") > initialLines
	})

	input <- 'q'
	waitDone(t, done)

	if gotInterval != refreshIntervalPlain {
		t.Fatalf("非 TTY 刷新间隔 = %v，期望 %v", gotInterval, refreshIntervalPlain)
	}
	if !fake.stopped.Load() {
		t.Fatal("Run 返回后 ticker 未 Stop")
	}
}

// 已关闭的输入通道只让面板停止收键，不能自行退出。
func TestRunClosedInputKeepsRunningUntilCancel(t *testing.T) {
	out := &lockedBuffer{}
	input := make(chan byte)
	close(input)
	cancel, done := runAsync(t, Options{Version: "v1", Input: input, Out: out})
	waitForOutput(t, out)

	select {
	case err := <-done:
		t.Fatalf("输入关闭后面板不应退出，实际返回 %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	waitDone(t, done)
}

// 非 TTY（测试里 Out 是内存 buffer）只打印单行状态，且绝不出现回车符。
func TestRunNonTTYOneLine(t *testing.T) {
	out := &lockedBuffer{}
	cancel, done := runAsync(t, Options{
		Version:   "v9.8.7",
		ProxyAddr: "127.0.0.1:38457",
		UIAddr:    "127.0.0.1:38470",
		Intercept: "已关闭",
		Out:       out,
	})

	first := waitForOutput(t, out)
	for _, want := range []string{"v9.8.7", "127.0.0.1:38457", "http://127.0.0.1:38470/", "已运行"} {
		if !strings.Contains(first, want) {
			t.Fatalf("首行状态应包含 %q，实际 %q", want, first)
		}
	}
	cancel()
	waitDone(t, done)

	got := out.String()
	if strings.Contains(got, "\r") {
		t.Fatalf("非 TTY 输出不应包含回车符：%q", got)
	}
	if n := strings.Count(got, "\n"); n != 1 {
		t.Fatalf("非 TTY 首屏应只有一行，实际 %d 行：%q", n, got)
	}
}

// AutoOpen 决定面板启动时是否自动打开浏览器界面（--no-open 由 CLI 置 false）。
func TestRunAutoOpen(t *testing.T) {
	cases := []struct {
		name string
		auto bool
		want int32
	}{
		{"默认自动打开一次", true, 1},
		{"--no-open 不自动打开", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var opened atomic.Int32
			out := &lockedBuffer{}
			cancel, done := runAsync(t, Options{
				Version:   "v1",
				ProxyAddr: "127.0.0.1:38457",
				UIAddr:    "127.0.0.1:38470",
				AutoOpen:  tc.auto,
				OpenUI:    func() error { opened.Add(1); return nil },
				Out:       out,
			})
			waitForOutput(t, out)
			cancel()
			waitDone(t, done)
			if got := opened.Load(); got != tc.want {
				t.Fatalf("OpenUI 调用 %d 次，期望 %d 次", got, tc.want)
			}
		})
	}
}

// 自动打开失败不能挡住面板本身。
func TestRunAutoOpenFailureKeepsPanel(t *testing.T) {
	out := &lockedBuffer{}
	cancel, done := runAsync(t, Options{
		Version:   "v1",
		ProxyAddr: "127.0.0.1:38457",
		UIAddr:    "127.0.0.1:38470",
		AutoOpen:  true,
		OpenUI:    func() error { return errForTest },
		Out:       out,
	})
	waitForOutput(t, out)
	cancel()
	waitDone(t, done)
	if got := out.String(); !strings.Contains(got, "打开界面失败") {
		t.Fatalf("自动打开失败应提示到面板，实际 %q", got)
	}
}
