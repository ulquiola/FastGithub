//go:build windows

package dashboard

import (
	"bufio"
	"context"
	"io"
	"os"
	"syscall"
	"unsafe"
)

// STD_INPUT_HANDLE / STD_OUTPUT_HANDLE 是 GetStdHandle 的 DWORD 参数；按有符号
// 32 位传入，32/64 位下都能得到正确的 0xFFFFFFF6 / 0xFFFFFFF5。
const (
	stdInputHandle  = -10
	stdOutputHandle = -11

	// 控制台输入模式（STD_INPUT_HANDLE）。
	consoleLineInput = 0x0002
	consoleEchoInput = 0x0004
	// 控制台输出模式（STD_OUTPUT_HANDLE）。
	consoleVTProcessing = 0x0004

	invalidHandle = ^uintptr(0)

	waitObject0    = 0
	waitPollMillis = 200
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetStdHandle        = kernel32.NewProc("GetStdHandle")
	procGetConsoleMode      = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode      = kernel32.NewProc("SetConsoleMode")
	procWaitForSingleObject = kernel32.NewProc("WaitForSingleObject")
)

// stdHandle 把 GetStdHandle 的负数常量转成句柄参数（有符号扩展）。
func stdHandle(id int32) uintptr { return uintptr(id) }

// readKeys 把按键写进 keys；ctx 取消或输入结束时返回并关闭通道。
//
// 有控制台时关闭行缓冲与回显实现“按下即响应”；stdin 被重定向（管道、文件、
// 服务会话）时退化为按字节读取，这样 `echo q | fastgithub` 也能退出面板。
func readKeys(ctx context.Context, keys chan<- byte) {
	defer close(keys)
	if h, mode, ok := consoleInputHandle(); ok {
		if restore, ok := enableSingleKey(h, mode); ok {
			defer restore()
			readConsoleKeys(ctx, h, keys)
			return
		}
	}
	readStreamKeys(ctx, os.Stdin, keys)
}

// consoleInputHandle 返回控制台输入句柄与当前模式；stdin 不是控制台时 ok 为 false。
func consoleInputHandle() (syscall.Handle, uint32, bool) {
	h, _, _ := procGetStdHandle.Call(stdHandle(stdInputHandle))
	if h == 0 || h == invalidHandle {
		return 0, 0, false
	}
	var mode uint32
	if r, _, _ := procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode))); r == 0 {
		return 0, 0, false
	}
	return syscall.Handle(h), mode, true
}

// enableSingleKey 关闭行缓冲与回显（保留 ENABLE_PROCESSED_INPUT，Ctrl+C 仍由
// 系统产生中断事件交给 signal.NotifyContext）；返回恢复原模式的函数。
func enableSingleKey(h syscall.Handle, mode uint32) (func(), bool) {
	newMode := mode &^ uint32(consoleLineInput|consoleEchoInput)
	if r, _, _ := procSetConsoleMode.Call(uintptr(h), uintptr(newMode)); r == 0 {
		return nil, false
	}
	return func() { procSetConsoleMode.Call(uintptr(h), uintptr(mode)) }, true
}

// readConsoleKeys 用 WaitForSingleObject 轮询句柄，保证 ctx 取消后 200ms 内退出，
// 避免阻塞在 ReadFile 上无法响应退出。
func readConsoleKeys(ctx context.Context, h syscall.Handle, keys chan<- byte) {
	var buf [16]byte
	for {
		if ctx.Err() != nil {
			return
		}
		r, _, _ := procWaitForSingleObject.Call(uintptr(h), waitPollMillis)
		if ctx.Err() != nil {
			return
		}
		if r != waitObject0 {
			continue
		}
		var n uint32
		if err := syscall.ReadFile(h, buf[:], &n, nil); err != nil || n == 0 {
			return
		}
		if !sendKeys(ctx, keys, buf[:n]) {
			return
		}
	}
}

// readStreamKeys 从任意 reader 逐字节读取；EOF 时返回。
func readStreamKeys(ctx context.Context, r io.Reader, keys chan<- byte) {
	reader := bufio.NewReader(r)
	buf := make([]byte, 1)
	for {
		n, err := reader.Read(buf)
		if n > 0 && !sendKeys(ctx, keys, buf[:n]) {
			return
		}
		if err != nil {
			return
		}
	}
}

// isTTY 报告 out 是否为本进程的控制台输出；重定向到管道/文件时为 false。
func isTTY(out io.Writer) bool {
	if out != io.Writer(os.Stdout) && out != io.Writer(os.Stderr) {
		return false
	}
	h, _, _ := procGetStdHandle.Call(stdHandle(stdOutputHandle))
	if h == 0 || h == invalidHandle {
		return false
	}
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode)))
	return r != 0
}

// enableVT 为控制台输出开启 ANSI 转义序列；失败时 Run 退回单行模式，不写回车。
func enableVT() bool {
	h, _, _ := procGetStdHandle.Call(stdHandle(stdOutputHandle))
	if h == 0 || h == invalidHandle {
		return false
	}
	var mode uint32
	if r, _, _ := procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode))); r == 0 {
		return false
	}
	if mode&consoleVTProcessing != 0 {
		return true
	}
	if r, _, _ := procSetConsoleMode.Call(h, uintptr(mode|consoleVTProcessing)); r == 0 {
		return false
	}
	return true
}
