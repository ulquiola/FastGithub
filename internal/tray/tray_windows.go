//go:build windows

package tray

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// 通知区域图标、隐藏消息窗口与菜单用到的常量。
const (
	trayMessage     = 0x8000 + 1 // WM_APP+1：Shell_NotifyIconW 的回调消息
	trayIconID      = 1
	trayWindowClass = "FastGithubTrayWindow"
	trayWindowTitle = "FastGithub"

	nimAdd        = 0x00000000
	nimModify     = 0x00000001
	nimDelete     = 0x00000002
	nimSetVersion = 0x00000004

	nifMessage = 0x00000001
	nifIcon    = 0x00000002
	nifTip     = 0x00000004
	nifShowTip = 0x00000080

	notifyIconVersion4 = 4

	wmNull          = 0x0000
	wmDestroy       = 0x0002
	wmClose         = 0x0010
	wmContextMenu   = 0x007B
	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205

	ninSelect    = 0x0400 // WM_USER
	ninKeySelect = 0x0401

	mfString    = 0x00000000
	mfGrayed    = 0x00000001
	mfChecked   = 0x00000008
	mfSeparator = 0x00000800

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	cfUnicodeText = 13
	gmemMoveable  = 0x0002

	swHide       = 0
	swShowNormal = 1

	idiApplication = 32512

	mbOK          = 0x00000000
	mbIconWarning = 0x00000030

	tokenElevation = 20

	errorAlreadyExists      = syscall.Errno(183)
	errorClassAlreadyExists = syscall.Errno(1410)
	errorCancelled          = syscall.Errno(1223) // ERROR_CANCELLED：用户取消 UAC
)

// point 对应 Win32 POINT（两个 32 位有符号整数）。
type point struct {
	X int32
	Y int32
}

// msg 对应 Win32 MSG；显式补齐 32 位对齐空洞，保证 x64 下为 48 字节。
type msg struct {
	HWnd    syscall.Handle
	Message uint32
	_       uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

// wndClassEx 对应 Win32 WNDCLASSEXW（x64 下 80 字节）。
type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   syscall.Handle
	Icon       syscall.Handle
	Cursor     syscall.Handle
	Background syscall.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     syscall.Handle
}

// notifyIconData 对应 Win32 NOTIFYICONDATAW（Vista+，x64 下 976 字节）。
// 字段顺序必须与 SDK 完全一致，cbSize 直接传 unsafe.Sizeof。
type notifyIconData struct {
	CbSize           uint32
	HWnd             syscall.Handle
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            syscall.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     syscall.Handle
}

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procRegisterWindowMessageW   = user32.NewProc("RegisterWindowMessageW")
	procIsWindow                 = user32.NewProc("IsWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	procAppendMenuW              = user32.NewProc("AppendMenuW")
	procDestroyMenu              = user32.NewProc("DestroyMenu")
	procTrackPopupMenu           = user32.NewProc("TrackPopupMenu")
	procCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon              = user32.NewProc("DestroyIcon")
	procLoadIconW                = user32.NewProc("LoadIconW")
	procMessageBoxW              = user32.NewProc("MessageBoxW")
	procOpenClipboard            = user32.NewProc("OpenClipboard")
	procEmptyClipboard           = user32.NewProc("EmptyClipboard")
	procSetClipboardData         = user32.NewProc("SetClipboardData")
	procCloseClipboard           = user32.NewProc("CloseClipboard")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procCreateMutexW     = kernel32.NewProc("CreateMutexW")
	procCloseHandle      = kernel32.NewProc("CloseHandle")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procGlobalFree       = kernel32.NewProc("GlobalFree")
	procRtlMoveMemory    = kernel32.NewProc("RtlMoveMemory")

	procShell_NotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procShellExecuteW     = shell32.NewProc("ShellExecuteW")

	procGetTokenInformation = advapi32.NewProc("GetTokenInformation")
)

// trayWindow 保存消息窗口与可变的菜单状态（开机启动勾选态会随点击变化）。
type trayWindow struct {
	hwnd  syscall.Handle
	hicon syscall.Handle
	opts  Options
}

// activeWindow 供 WndProc 访问；托盘 Run 已 LockOSThread，进程内同时只有一个实例。
var activeWindow *trayWindow

// taskbarCreatedMessage 是 Explorer 重启后广播的注册消息；0 表示注册失败。
// 收到它时需要重新添加图标，否则 Explorer 重启后托盘图标永久消失。
var taskbarCreatedMessage = registerTaskbarCreatedMessage()

func registerTaskbarCreatedMessage() uint32 {
	name, err := syscall.UTF16PtrFromString("TaskbarCreated")
	if err != nil {
		return 0
	}
	r, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(name)))
	return uint32(r)
}

// shellNotifyIcon 是对 Shell_NotifyIconW 的唯一封装，测试中可替换为成功且记录
// 消息的假实现，从而在没有 explorer.exe 的会话里验证 Run 的清理路径。
var shellNotifyIcon = shellNotifyIconW

func shellNotifyIconW(message uint32, nid *notifyIconData) (bool, error) {
	r, _, errno := procShell_NotifyIconW.Call(uintptr(message), uintptr(unsafe.Pointer(nid)))
	if r == 0 {
		if errno == nil {
			return false, errors.New("Shell_NotifyIconW 返回 FALSE")
		}
		return false, errno
	}
	return true, nil
}

// instanceMutexName 与回退名可在测试中替换，避免与真实托盘实例互相影响。
var (
	instanceMutexName         = `Global\FastGithubTray`
	instanceFallbackMutexName = `Local\FastGithubTray`
)

// AcquireSingleInstance 在启动代理前抢占单实例互斥体，命中时返回 ErrAlreadyRunning。
// 返回的 release 必须在本进程退出前调用；重复调用是安全的。
func AcquireSingleInstance() (release func(), err error) {
	mutex, already, err := createInstanceMutex(instanceMutexName)
	if err != nil {
		// 普通用户可能没有创建 Global\ 命名对象的权限，退回当前会话命名。
		mutex, already, err = createInstanceMutex(instanceFallbackMutexName)
	}
	if err != nil {
		return nil, fmt.Errorf("创建单实例互斥体失败: %w", err)
	}
	if already {
		return nil, ErrAlreadyRunning
	}
	if mutex == 0 {
		return func() {}, nil
	}
	var once sync.Once
	return func() {
		once.Do(func() { procCloseHandle.Call(uintptr(mutex)) })
	}, nil
}

// Run 在通知区域显示图标并进入消息循环，直到 ctx 取消或用户选择退出。
//
// 单实例互斥体不在这里获取：调用方应在启动代理之前调用 AcquireSingleInstance，
// 否则第二个实例会先绑定端口失败、再发现已有实例。
func Run(ctx context.Context, o Options) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hicon, ownedIcon := loadAppIcon()
	if hicon != 0 && ownedIcon {
		defer procDestroyIcon.Call(uintptr(hicon))
	}

	hwnd, err := createMessageWindow(hicon)
	if err != nil {
		return err
	}

	w := &trayWindow{hwnd: hwnd, hicon: hicon, opts: o}
	activeWindow = w
	defer func() { activeWindow = nil }()

	if err := addNotifyIcon(hwnd, hicon, StatusText(o)); err != nil {
		destroyWindow(hwnd)
		return err
	}

	// ctx 取消（Ctrl+C、服务停止或 Actions.Quit）时让消息循环退出。
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			procPostMessageW.Call(uintptr(hwnd), wmClose, 0, 0)
		case <-done:
		}
	}()

	err = runMessageLoop()
	// 正常退出时图标已在 WM_CLOSE 里删除；这里再删一次是幂等的兜底。
	removeNotifyIcon(hwnd)
	destroyWindow(hwnd)
	return err
}

// createInstanceMutex 创建命名互斥体；already 为 true 表示已有实例持有它。
func createInstanceMutex(name string) (handle syscall.Handle, already bool, err error) {
	namePtr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, false, err
	}
	h, _, errno := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(namePtr)))
	if h == 0 {
		return 0, false, errno
	}
	if errno == errorAlreadyExists {
		procCloseHandle.Call(h)
		return 0, true, nil
	}
	return syscall.Handle(h), false, nil
}

func createMessageWindow(hicon syscall.Handle) (syscall.Handle, error) {
	instance, _, _ := procGetModuleHandleW.Call(0)
	className, err := syscall.UTF16PtrFromString(trayWindowClass)
	if err != nil {
		return 0, err
	}
	title, err := syscall.UTF16PtrFromString(trayWindowTitle)
	if err != nil {
		return 0, err
	}
	wc := wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   syscall.NewCallback(wndProc),
		Instance:  syscall.Handle(instance),
		Icon:      hicon,
		IconSm:    hicon,
		ClassName: className,
	}
	if atom, _, errno := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); atom == 0 && errno != errorClassAlreadyExists {
		return 0, fmt.Errorf("注册托盘窗口类失败: %w", errno)
	}
	hwnd, _, errno := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(title)),
		0, // 无样式、不可见；普通顶层窗口便于菜单获得前台焦点。
		0, 0, 0, 0,
		0, 0,
		instance,
		0,
	)
	if hwnd == 0 {
		return 0, fmt.Errorf("创建托盘消息窗口失败: %w", errno)
	}
	return syscall.Handle(hwnd), nil
}

// wndProc 是隐藏窗口的消息处理函数，必须用 syscall.NewCallback 注册。
func wndProc(hwnd syscall.Handle, message uint32, wParam, lParam uintptr) uintptr {
	if taskbarCreatedMessage != 0 && message == taskbarCreatedMessage {
		handleTaskbarCreated()
		return 0
	}
	switch message {
	case trayMessage:
		// NOTIFYICON_VERSION_4 把通知码放在 lParam 低 16 位；旧版直接就是
		// 鼠标消息，两种格式取低 16 位都成立。
		handleTrayEvent(uint32(lParam & 0xffff))
		return 0
	case wmClose:
		removeNotifyIcon(hwnd)
		destroyWindow(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(message), wParam, lParam)
	return r
}

// handleTaskbarCreated 在 Explorer 重启后重新添加图标。
// 失败无处提示，也不应打断消息循环；下一次 Explorer 重启会再次尝试。
func handleTaskbarCreated() {
	w := activeWindow
	if w == nil || w.hwnd == 0 {
		return
	}
	_ = addNotifyIcon(w.hwnd, w.hicon, StatusText(w.opts))
}

func handleTrayEvent(event uint32) {
	switch event {
	case wmRButtonUp, wmContextMenu:
		showContextMenu()
	case wmLButtonDblClk, ninSelect, ninKeySelect:
		// 版本 4 下左键单击回调是 NIN_SELECT、双击是 WM_LBUTTONDBLCLK，两者都打开界面。
		openUI()
	}
}

func showContextMenu() {
	w := activeWindow
	if w == nil {
		return
	}
	// 在“退出”前插入分隔线，其余项顺序与 MenuItems 一致。
	items := make([]Item, 0, len(MenuItems(w.opts))+1)
	for _, it := range MenuItems(w.opts) {
		if it.ID == IDQuit {
			items = append(items, Item{Separator: true})
		}
		items = append(items, it)
	}

	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	for _, it := range items {
		if it.Separator {
			procAppendMenuW.Call(menu, mfSeparator, 0, 0)
			continue
		}
		flags := uintptr(mfString)
		if !it.Enabled {
			flags |= mfGrayed
		}
		if it.Checked {
			flags |= mfChecked
		}
		label, err := syscall.UTF16PtrFromString(it.Label)
		if err != nil {
			continue
		}
		procAppendMenuW.Call(menu, flags, uintptr(it.ID), uintptr(unsafe.Pointer(label)))
	}

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// 必须先置前台，否则点击菜单外部时菜单不会消失。
	procSetForegroundWindow.Call(uintptr(w.hwnd))
	cmd, _, _ := procTrackPopupMenu.Call(
		menu,
		tpmReturnCmd|tpmRightButton,
		uintptr(pt.X), uintptr(pt.Y),
		0,
		uintptr(w.hwnd),
		0,
	)
	// 微软文档要求 TrackPopupMenu 之后向窗口投递一条空消息，修复菜单残留。
	procPostMessageW.Call(uintptr(w.hwnd), wmNull, 0, 0)
	if cmd == 0 {
		return
	}
	w.handleCommand(int(cmd))
}

func (w *trayWindow) handleCommand(id int) {
	switch id {
	case IDOpenUI:
		openUI()
	case IDCopyProxy:
		var err error
		if w.opts.Actions.CopyProxy != nil {
			err = w.opts.Actions.CopyProxy()
		} else {
			err = copyText(w.hwnd, w.opts.ProxyAddr)
		}
		if err != nil {
			showError(w.hwnd, "复制代理地址失败: "+err.Error())
		}
	case IDInstallCert:
		var err error
		if w.opts.Actions.InstallCert != nil {
			err = w.opts.Actions.InstallCert()
		} else {
			err = installCertElevated(w.hwnd)
		}
		if err != nil {
			showError(w.hwnd, "安装证书失败: "+err.Error())
		}
	case IDInterceptState:
		// 仅展示状态：运行中切换拦截需要重启代理，当前版本只通过配置启停。
	case IDAutostart:
		enabled := !w.opts.Autostart
		if err := SetAutostart(enabled, w.opts.ConfigPath); err != nil {
			showError(w.hwnd, "设置开机启动失败: "+err.Error())
			return
		}
		w.opts.Autostart = enabled
	case IDQuit:
		if w.opts.Actions.Quit != nil {
			w.opts.Actions.Quit()
		}
		procPostMessageW.Call(uintptr(w.hwnd), wmClose, 0, 0)
	}
}

func openUI() {
	w := activeWindow
	if w == nil {
		return
	}
	if w.opts.Actions.OpenUI != nil {
		w.opts.Actions.OpenUI()
		return
	}
	url := uiURL(w.opts.UIAddr)
	if url == "" {
		return
	}
	if err := shellExecute(w.hwnd, "open", url, "", swShowNormal); err != nil {
		showError(w.hwnd, "打开界面失败: "+err.Error())
	}
}

// installCertElevated 以管理员身份重新运行自身 install-cert；用户取消 UAC 视为无操作。
func installCertElevated(hwnd syscall.Handle) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位可执行文件失败: %w", err)
	}
	err = shellExecute(hwnd, "runas", exe, "install-cert", swHide)
	if err == nil {
		return nil
	}
	var execErr *shellExecuteError
	if errors.As(err, &execErr) && execErr.cancelled() {
		return nil
	}
	return fmt.Errorf("以管理员身份启动 install-cert 失败: %w", err)
}

// shellExecute 可在测试中替换，避免真的弹出 UAC 对话框。
var shellExecute = shellExecuteImpl

// shellExecuteError 保留 ShellExecuteW 的返回码与 GetLastError，便于区分用户取消。
type shellExecuteError struct {
	op    string
	code  uintptr
	errno syscall.Errno
}

func (e *shellExecuteError) Error() string {
	if e.errno != 0 {
		return fmt.Sprintf("ShellExecuteW(%s) 失败（代码 %d）: %v", e.op, e.code, e.errno)
	}
	return fmt.Sprintf("ShellExecuteW(%s) 失败（代码 %d）", e.op, e.code)
}

// cancelled 报告用户是否在 UAC 提示中点了取消。
func (e *shellExecuteError) cancelled() bool {
	return e.code == uintptr(errorCancelled) || e.errno == errorCancelled
}

func shellExecuteImpl(hwnd syscall.Handle, op, file, params string, show int32) error {
	opPtr, err := syscall.UTF16PtrFromString(op)
	if err != nil {
		return err
	}
	filePtr, err := syscall.UTF16PtrFromString(file)
	if err != nil {
		return err
	}
	var paramsPtr *uint16
	if params != "" {
		if paramsPtr, err = syscall.UTF16PtrFromString(params); err != nil {
			return err
		}
	}
	r, _, errno := procShellExecuteW.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(opPtr)),
		uintptr(unsafe.Pointer(filePtr)),
		uintptr(unsafe.Pointer(paramsPtr)),
		0,
		uintptr(show),
	)
	if r <= 32 {
		code, _ := errno.(syscall.Errno)
		return &shellExecuteError{op: op, code: r, errno: code}
	}
	return nil
}

func showError(hwnd syscall.Handle, text string) {
	title, _ := syscall.UTF16PtrFromString("FastGithub")
	body, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	procMessageBoxW.Call(
		uintptr(hwnd),
		uintptr(unsafe.Pointer(body)),
		uintptr(unsafe.Pointer(title)),
		mbOK|mbIconWarning,
	)
}

func copyText(hwnd syscall.Handle, text string) error {
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	if ok, _, errno := procOpenClipboard.Call(uintptr(hwnd)); ok == 0 {
		return fmt.Errorf("打开剪贴板失败: %w", errno)
	}
	defer procCloseClipboard.Call()

	if ok, _, _ := procEmptyClipboard.Call(); ok == 0 {
		return errors.New("清空剪贴板失败")
	}
	size := uintptr(len(utf16) * 2)
	h, _, errno := procGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return fmt.Errorf("分配剪贴板内存失败: %w", errno)
	}
	ptr, _, _ := procGlobalLock.Call(h)
	if ptr == 0 {
		procGlobalFree.Call(h)
		return errors.New("锁定剪贴板内存失败")
	}
	// 用 RtlMoveMemory 拷贝，避免把 GlobalLock 返回的 uintptr 转成 unsafe.Pointer。
	procRtlMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&utf16[0])), size)
	runtime.KeepAlive(utf16)
	procGlobalUnlock.Call(h)
	if ok, _, _ := procSetClipboardData.Call(cfUnicodeText, h); ok == 0 {
		procGlobalFree.Call(h)
		return errors.New("写入剪贴板失败")
	}
	// SetClipboardData 成功后内存归系统所有，不能再 GlobalFree。
	return nil
}

func addNotifyIcon(hwnd, hicon syscall.Handle, tip string) error {
	nid := notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             hwnd,
		UID:              trayIconID,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: trayMessage,
		HIcon:            hicon,
	}
	setFixedUTF16(nid.SzTip[:], tip)
	if ok, err := shellNotifyIcon(nimAdd, &nid); !ok {
		return fmt.Errorf("添加通知区域图标失败: %w", err)
	}
	nid.UVersion = notifyIconVersion4
	// 设置版本失败不致命：图标已经加上，只是回调保持旧格式，事件处理两者都兼容。
	shellNotifyIcon(nimSetVersion, &nid)
	nid.UFlags = nifShowTip
	shellNotifyIcon(nimModify, &nid)
	return nil
}

func removeNotifyIcon(hwnd syscall.Handle) {
	nid := notifyIconData{
		CbSize: uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:   hwnd,
		UID:    trayIconID,
	}
	shellNotifyIcon(nimDelete, &nid)
}

// setFixedUTF16 把 s 写进定长 UTF-16 缓冲区，保证以 NUL 结尾且不越界。
func setFixedUTF16(dst []uint16, s string) {
	utf16, err := syscall.UTF16FromString(s)
	if err != nil {
		dst[0] = 0
		return
	}
	if len(utf16) > len(dst) {
		utf16 = utf16[:len(dst)]
		utf16[len(utf16)-1] = 0
	}
	copy(dst, utf16)
}

func runMessageLoop() error {
	var m msg
	for {
		r, _, errno := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 {
			return fmt.Errorf("GetMessageW 失败: %w", errno)
		}
		if r == 0 {
			return nil
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func destroyWindow(hwnd syscall.Handle) {
	procDestroyWindow.Call(uintptr(hwnd))
}

// IsAdmin 报告当前进程是否以提升后的管理员令牌运行。
func IsAdmin() bool {
	process, _ := syscall.GetCurrentProcess()
	var token syscall.Token
	if err := syscall.OpenProcessToken(process, syscall.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()
	var elevated uint32
	var size uint32
	r, _, _ := procGetTokenInformation.Call(
		uintptr(token),
		tokenElevation,
		uintptr(unsafe.Pointer(&elevated)),
		unsafe.Sizeof(elevated),
		uintptr(unsafe.Pointer(&size)),
	)
	return r != 0 && elevated != 0
}

// CopyToClipboard 把 text 写入系统剪贴板（CF_UNICODETEXT）。
// 控制台面板没有托盘窗口，传 hwnd=0 让系统按当前前台窗口归属剪贴板。
func CopyToClipboard(text string) error {
	return copyText(0, text)
}

// OpenURL 用系统默认浏览器打开 url。
func OpenURL(url string) error {
	if url == "" {
		return errors.New("URL 为空")
	}
	// 直接交给系统默认浏览器。注意不要用 explorer.exe <url>：每次调用都会新拉一个
	// explorer 进程，在本机会因 0xc0000142 初始化失败并弹出“应用程序无法正常启动”。
	return shellExecute(0, "open", url, "", swShowNormal)
}

// RunElevated 以管理员身份重新运行自身并传入 params（会触发 UAC）。
// 用户取消 UAC 属于主动放弃，返回 nil 而不是错误。
func RunElevated(params string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位可执行文件失败: %w", err)
	}
	err = shellExecute(0, "runas", exe, params, swHide)
	if err == nil {
		return nil
	}
	var execErr *shellExecuteError
	if errors.As(err, &execErr) && execErr.cancelled() {
		return nil
	}
	return fmt.Errorf("以管理员身份启动 %s 失败: %w", params, err)
}

// RelaunchElevated 以管理员身份重新运行当前程序并传入 args（会触发 UAC）。
// 用户取消时返回明确错误，调用方应终止而不是静默继续。
func RelaunchElevated(args []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("定位可执行文件失败: %w", err)
	}
	err = shellExecute(0, "runas", exe, quoteArgs(args), swShowNormal)
	if err == nil {
		return nil
	}
	var execErr *shellExecuteError
	if errors.As(err, &execErr) && execErr.cancelled() {
		return errors.New("用户取消了管理员权限请求")
	}
	return fmt.Errorf("请求管理员权限失败: %w", err)
}

func quoteArgs(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		parts[i] = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
	}
	return strings.Join(parts, " ")
}
