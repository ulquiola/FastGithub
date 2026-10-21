// Package dashboard 提供没有通知区域（托盘）时的常驻控制台状态面板。
//
// 面板显示版本、代理/UI 地址、透明拦截状态与运行时长，并支持单键操作：
// 1 打开界面、2 复制代理地址、3 提权安装证书、q 退出。Windows 上关闭行缓冲实现
// 单键响应；stdin 被重定向或无控制台的会话（例如没有 Explorer 的服务会话）
// 退化为按字节读取，其它平台按行读取并映射首字符。
package dashboard

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	// refreshInterval 是交互式（TTY）模式下的原地刷新间隔。
	refreshInterval = 2 * time.Second
	// refreshIntervalPlain 是非 TTY 模式的打印间隔：不刷屏，只留一行心跳。
	refreshIntervalPlain = 30 * time.Second

	// ctrlC 是管道输入里的 Ctrl+C 字节。
	ctrlC = 0x03
)

// refreshTicker 隔离刷新定时器，测试可注入假实现并断言 Stop 被调用。
type refreshTicker interface {
	C() <-chan time.Time
	Stop()
}

type systemRefreshTicker struct{ ticker *time.Ticker }

func (t systemRefreshTicker) C() <-chan time.Time { return t.ticker.C }
func (t systemRefreshTicker) Stop()               { t.ticker.Stop() }

var newRefreshTicker = func(interval time.Duration) refreshTicker {
	return systemRefreshTicker{ticker: time.NewTicker(interval)}
}

// Options 是面板运行所需的只读快照。
//
// AutoOpen 表达 --no-open（启动时是否自动打开浏览器界面）；Out 让调用方与测试
// 指定输出目标；Input 让测试注入按键通道，nil 时使用平台输入源。
type Options struct {
	Version     string
	ProxyAddr   string
	UIAddr      string
	Intercept   string // "已启用" / "未启用（需要管理员）" / "已关闭"
	OpenUI      func() error
	CopyProxy   func() error
	InstallCert func() error
	AutoOpen    bool
	Out         io.Writer
	// Input 是读键测试缝；nil 时由平台实现打开输入源。Run 不会关闭该通道。
	Input <-chan byte
}

// StatusText 返回面板状态区块；uptime 显示为 HH:MM:SS（小时可超过 24）。
func StatusText(o Options, uptime time.Duration) string {
	version := o.Version
	if version == "" {
		version = "dev"
	}
	intercept := o.Intercept
	if intercept == "" {
		intercept = "已关闭"
	}
	lines := []string{
		"FastGithub " + version + "   代理 " + orDash(o.ProxyAddr) + "   UI " + uiURL(o.UIAddr),
		"透明拦截：" + intercept + "        已运行 " + formatUptime(uptime),
		"",
		"[1] 打开控制台界面   [2] 复制代理地址   [3] 安装证书   [q] 退出",
	}
	return strings.Join(lines, "\n")
}

// oneLineStatus 把状态区块压成一行，供非 TTY 输出使用：不产生回车刷屏。
func oneLineStatus(o Options, uptime time.Duration) string {
	lines := strings.Split(StatusText(o, uptime), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "  ")
}

// formatUptime 把运行时长格式化成 HH:MM:SS；小时不取模，负数按 0 处理。
func formatUptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	total := int64(d / time.Second)
	return fmt.Sprintf("%02d:%02d:%02d", total/3600, total%3600/60, total%60)
}

// uiURL 把监听地址转成浏览器可打开的 URL。
func uiURL(addr string) string {
	if addr == "" {
		return "-"
	}
	if strings.Contains(addr, "://") {
		if strings.HasSuffix(addr, "/") {
			return addr
		}
		return addr + "/"
	}
	return "http://" + addr + "/"
}

// orDash 让空地址在面板上显示为占位符，而不是留白。
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// HandleKey 分发单键命令并返回是否请求退出。
//
// 它是纯函数：除调用 Options 里的回调外没有副作用，未知按键什么也不做。
func HandleKey(o Options, key byte) (quit bool) {
	switch key {
	case '1':
		callAction(o.OpenUI)
	case '2':
		callAction(o.CopyProxy)
	case '3':
		callAction(o.InstallCert)
	case 'q', 'Q', ctrlC:
		return true
	}
	return false
}

// callAction 调用回调并丢弃错误：错误展示由 Run 包装后的回调负责，
// 这样 HandleKey 保持无副作用、易测。
func callAction(fn func() error) {
	if fn != nil {
		_ = fn()
	}
}

// Run 显示状态面板并进入按键循环，直到 ctx 取消、用户按 q 或输入结束。
//
// 启动时按 AutoOpen 自动打开一次浏览器界面，然后打印状态区块。TTY 下每 2 秒
// 原地刷新；非 TTY（重定向、服务会话）每 30 秒打印一行，绝不写回车符。
func Run(ctx context.Context, o Options) error {
	out := o.Out
	if out == nil {
		out = os.Stdout
	}

	// 按键反馈先收集起来，等状态区块原地刷新后统一打印在区块下方；
	// 下一帧先清除这些反馈行，避免它们与重绘的面板叠成两份。
	var feedbackLines []string
	captureFeedback := false
	report := func(message string) {
		if captureFeedback {
			feedbackLines = append(feedbackLines, message)
			return
		}
		fmt.Fprintln(out, message)
	}
	o = withFeedback(withDefaults(o), report)

	if o.AutoOpen {
		// 打不开浏览器不能影响面板本身，失败原因由 withFeedback 打印。
		_ = o.OpenUI()
	}

	start := time.Now()
	interactive := isTTY(out) && enableVT(out)
	interval := refreshInterval
	if !interactive {
		interval = refreshIntervalPlain
	}

	// lastLines 记录面板区块的行数；feedbackPrinted 记录区块下方反馈的行数。
	// 每次重绘先清除旧反馈，再上移重绘面板，避免反馈与面板叠成两份。
	lastLines := 0
	feedbackPrinted := 0
	clearFeedback := func() {
		if !interactive || feedbackPrinted == 0 {
			return
		}
		var b strings.Builder
		fmt.Fprintf(&b, "\x1b[%dA", feedbackPrinted)
		for i := 0; i < feedbackPrinted; i++ {
			b.WriteString("\r\x1b[2K")
			if i < feedbackPrinted-1 {
				b.WriteString("\n")
			}
		}
		if feedbackPrinted > 1 {
			fmt.Fprintf(&b, "\x1b[%dA", feedbackPrinted-1)
		}
		_, _ = io.WriteString(out, b.String())
		feedbackPrinted = 0
	}
	redraw := func() {
		if !interactive {
			fmt.Fprintln(out, oneLineStatus(o, time.Since(start)))
			return
		}
		clearFeedback()
		lines := strings.Split(StatusText(o, time.Since(start)), "\n")
		var b strings.Builder
		if lastLines > 0 {
			fmt.Fprintf(&b, "\x1b[%dA", lastLines)
		}
		for _, line := range lines {
			b.WriteString("\r\x1b[2K")
			b.WriteString(line)
			b.WriteString("\n")
		}
		_, _ = io.WriteString(out, b.String())
		lastLines = len(lines)
	}

	// 输入源的打开与恢复都由 Run 持有：即使按键处理 panic，控制台模式也会恢复。
	keys := o.Input
	if keys == nil {
		input := acquireInput()
		defer input.restore()
		readerCtx, stopReader := context.WithCancel(ctx)
		defer stopReader()
		keyCh := make(chan byte, 8)
		go input.readKeys(readerCtx, keyCh)
		keys = keyCh
	}

	redraw()
	ticker := newRefreshTicker(interval)
	defer ticker.Stop()
	ticks := ticker.C()
	captureFeedback = true

	for {
		select {
		case <-ctx.Done():
			if interactive {
				// 光标停在区块内，换行让 shell 提示符回到行首。
				fmt.Fprintln(out)
			}
			return nil
		case key, ok := <-keys:
			if !ok {
				// 输入结束（stdin 关闭）：面板继续常驻，只等 ctx 退出。
				keys = nil
				continue
			}
			feedbackLines = feedbackLines[:0]
			if HandleKey(o, key) {
				fmt.Fprintln(out, "正在退出 FastGithub...")
				if interactive {
					fmt.Fprintln(out)
				}
				return nil
			}
			redraw()
			for _, line := range feedbackLines {
				fmt.Fprintln(out, line)
			}
			if interactive {
				feedbackPrinted = len(feedbackLines)
			}
		case <-ticks:
			redraw()
		}
	}
}

// withDefaults 给未提供的回调补上平台默认动作（打开界面、复制、提权装证书）。
func withDefaults(o Options) Options {
	if o.OpenUI == nil {
		addr := o.UIAddr
		o.OpenUI = func() error { return openUI(addr) }
	}
	if o.CopyProxy == nil {
		addr := o.ProxyAddr
		o.CopyProxy = func() error { return copyProxy(addr) }
	}
	if o.InstallCert == nil {
		o.InstallCert = installCert
	}
	return o
}

// withFeedback 包装回调，把成功/失败结果交给面板统一输出，避免按键后毫无反馈。
func withFeedback(o Options, report func(string)) Options {
	o.OpenUI = feedback("已在浏览器打开界面", "打开界面失败", report, o.OpenUI)
	o.CopyProxy = feedback("已复制代理地址: "+o.ProxyAddr, "复制代理地址失败", report, o.CopyProxy)
	o.InstallCert = feedback("已请求管理员权限安装证书（请在 UAC 中确认）", "安装证书失败", report, o.InstallCert)
	return o
}

// feedback 给回调加上一行结果提示；回调为 nil 时保持 nil。
func feedback(success, failure string, report func(string), fn func() error) func() error {
	if fn == nil {
		return nil
	}
	return func() error {
		if err := fn(); err != nil {
			if report != nil {
				report(fmt.Sprintf("%s: %v", failure, err))
			}
			return nil
		}
		if success != "" && report != nil {
			report(success)
		}
		return nil
	}
}

// sendKeys 逐个投递按键并过滤换行；ctx 取消时返回 false。
func sendKeys(ctx context.Context, keys chan<- byte, data []byte) bool {
	for _, b := range data {
		if b == '\r' || b == '\n' {
			continue
		}
		select {
		case keys <- b:
		case <-ctx.Done():
			return false
		}
	}
	return true
}
