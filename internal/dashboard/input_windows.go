//go:build windows

package dashboard

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"os"
	"syscall"
	"unsafe"
)

// STD_INPUT_HANDLE 是 GetStdHandle 的 DWORD 参数；按有符号 32 位传入，
// 32/64 位下都能得到正确的 0xFFFFFFF6。
const (
	stdInputHandle = -10

	// 控制台输入模式（STD_INPUT_HANDLE）。
	consoleLineInput = 0x0002
	consoleEchoInput = 0x0004
	// 控制台输出模式（STD_OUTPUT_HANDLE）。
	consoleVTProcessing = 0x0004

	keyEventType = 0x0001

	invalidHandle = ^uintptr(0)
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetStdHandle      = kernel32.NewProc("GetStdHandle")
	procGetConsoleMode    = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode    = kernel32.NewProc("SetConsoleMode")
	procReadConsoleInputW = kernel32.NewProc("ReadConsoleInputW")
)

// inputRecord 对应 Win32 INPUT_RECORD（20 字节）。只读取 KEY_EVENT 的
// bKeyDown 与 UnicodeChar；其余事件类型复用同一块 16 字节 union。
type inputRecord struct {
	EventType uint16
	_         uint16
	Event     [16]byte
}

// inputSession 是 Run 持有的输入源。控制台会话记录原始模式，restore 负责还原；
// 管道/文件输入不修改任何模式。
type inputSession struct {
	handle  syscall.Handle
	mode    uint32
	console bool
}

// acquireInput 在启动读键 goroutine 之前捕获控制台句柄与原始模式，并关闭
// 行缓冲与回显。失败时退化为按字节读取 stdin。
func acquireInput() inputSession {
	h, mode, ok := consoleInputHandle()
	if !ok {
		return inputSession{}
	}
	newMode := mode &^ uint32(consoleLineInput|consoleEchoInput)
	if r, _, _ := procSetConsoleMode.Call(uintptr(h), uintptr(newMode)); r == 0 {
		return inputSession{}
	}
	return inputSession{handle: h, mode: mode, console: true}
}

// restore 把控制台恢复成 acquireInput 捕获的原始模式。
func (s inputSession) restore() {
	if !s.console {
		return
	}
	procSetConsoleMode.Call(uintptr(s.handle), uintptr(s.mode))
}

// readKeys 把按键写进 keys；ctx 取消或输入结束时返回并关闭通道。
func (s inputSession) readKeys(ctx context.Context, keys chan<- byte) {
	defer close(keys)
	if s.console {
		readConsoleKeys(ctx, s.handle, keys)
		return
	}
	readStreamKeys(ctx, os.Stdin, keys)
}

// stdHandle 把 GetStdHandle 的负数常量转成句柄参数（有符号扩展）。
func stdHandle(id int32) uintptr { return uintptr(id) }

// consoleInputHandle 返回控制台输入句柄与当前模式；stdin 不是控制台时 ok 为 false。
func consoleInputHandle() (syscall.Handle, uint32, bool) {
	h, _, _ := procGetStdHandle.Call(stdHandle(stdInputHandle))
	if h == 0 || h == invalidHandle {
		return 0, 0, false
	}
	mode, ok := consoleMode(h)
	if !ok {
		return 0, 0, false
	}
	return syscall.Handle(h), mode, true
}

// consoleMode 读取句柄的控制台模式；非控制台句柄返回 false。
func consoleMode(h uintptr) (uint32, bool) {
	var mode uint32
	if r, _, _ := procGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode))); r == 0 {
		return 0, false
	}
	return mode, true
}

// readConsoleKeys 用 ReadConsoleInputW 读取输入记录。它对每个记录（包括
// key-up）都会返回，避免字符模式 ReadFile 在无字符时永久阻塞；每次读取后
// 先检查 ctx，再投递过滤后的按键。
func readConsoleKeys(ctx context.Context, h syscall.Handle, keys chan<- byte) {
	var record inputRecord
	for {
		if ctx.Err() != nil {
			return
		}
		var n uint32
		r, _, _ := procReadConsoleInputW.Call(
			uintptr(h),
			uintptr(unsafe.Pointer(&record)),
			1,
			uintptr(unsafe.Pointer(&n)),
		)
		if r == 0 || n == 0 {
			return
		}
		if ctx.Err() != nil {
			return
		}
		key, ok := keyByteFromRecord(record)
		if !ok {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case keys <- key:
		}
	}
}

// keyByteFromRecord 只接受 KEY_EVENT 的 key-down 且 UnicodeChar 非 0 的记录。
func keyByteFromRecord(record inputRecord) (byte, bool) {
	if record.EventType != keyEventType {
		return 0, false
	}
	if binary.LittleEndian.Uint32(record.Event[0:4]) == 0 {
		return 0, false
	}
	ch := binary.LittleEndian.Uint16(record.Event[10:12])
	if ch == 0 || ch > 0x7F {
		return 0, false
	}
	return byte(ch), true
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

// isTTY 检查实际输出 writer 的控制台句柄；非 *os.File（管道/内存 writer）不是 TTY。
func isTTY(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	_, ok = consoleMode(f.Fd())
	return ok
}

// enableVT 为实际输出 writer 的控制台开启 ANSI 转义序列；失败时 Run 退回
// 单行模式，不写回车。
func enableVT(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	h := f.Fd()
	mode, ok := consoleMode(h)
	if !ok {
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
