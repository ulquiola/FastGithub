//go:build windows

package tray

import (
	"os"
	"testing"
	"unsafe"
)

func TestIconParse(t *testing.T) {
	data, err := os.ReadFile("assets/app.ico")
	if err != nil {
		t.Fatalf("读取图标失败: %v", err)
	}
	images, err := parseICO(data)
	if err != nil {
		t.Fatalf("解析图标失败: %v", err)
	}
	if len(images) == 0 {
		t.Fatal("应至少解析出一张图像")
	}
	found256 := false
	for i, img := range images {
		if img.width <= 0 || img.height <= 0 {
			t.Fatalf("第 %d 项尺寸非法: %dx%d", i, img.width, img.height)
		}
		if len(img.data) == 0 {
			t.Fatalf("第 %d 项没有像素数据", i)
		}
		if img.width == 256 {
			found256 = true
		}
	}
	// ICO 用 0 表示 256，解析时必须换算，否则会拿到宽度 0 的非法图像。
	if !found256 {
		t.Fatal("宽度字节为 0 的条目应解析为 256")
	}
}

func TestIconParseRejectsGarbage(t *testing.T) {
	valid, err := os.ReadFile("assets/app.ico")
	if err != nil {
		t.Fatalf("读取图标失败: %v", err)
	}
	cases := map[string][]byte{
		"nil":       nil,
		"too short": []byte{0, 0},
		"text":      []byte("not an icon at all"),
		"reserved":  {1, 0, 1, 0, 1, 0},
		"type":      {0, 0, 2, 0, 1, 0},
		"count 0":   {0, 0, 1, 0, 0, 0},
		"truncated": valid[:100],
		// 目录完整但数据偏移越界：22 字节头 + 一张声称位于 1000、大小 10 的图。
		"bad offset": {0, 0, 1, 0, 1, 0, 16, 16, 0, 0, 1, 0, 32, 0, 10, 0, 0, 0, 0xE8, 0x03, 0, 0},
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseICO(data); err == nil {
				t.Fatalf("垃圾数据应返回错误：%v", data)
			}
		})
	}
}

// CreateIconFromResourceEx 是托盘图标的关键 syscall；在无 explorer 的会话里也能
// 验证它接受我们的 ICO 数据并返回有效 HICON。
func TestCreateHICON(t *testing.T) {
	images, err := parseICO(appIconData)
	if err != nil {
		t.Fatalf("解析内嵌图标失败: %v", err)
	}
	img, err := selectIcon(images, 32)
	if err != nil {
		t.Fatalf("选择图标失败: %v", err)
	}
	hicon, err := createIcon(img)
	if err != nil {
		t.Fatalf("创建 HICON 失败: %v", err)
	}
	defer procDestroyIcon.Call(uintptr(hicon))
	if hicon == 0 {
		t.Fatal("HICON 句柄为空")
	}
}

func TestIconSelect(t *testing.T) {
	images := []iconImage{
		{width: 16, height: 16, data: make([]byte, 10)},
		{width: 32, height: 32, data: make([]byte, 20)},
		{width: 32, height: 32, data: make([]byte, 30)},
		{width: 48, height: 48, data: make([]byte, 40)},
		{width: 256, height: 256, data: make([]byte, 50)},
	}
	got, err := selectIcon(images, 32)
	if err != nil {
		t.Fatalf("selectIcon: %v", err)
	}
	if got.width != 32 || len(got.data) != 30 {
		t.Fatalf("应选 32x32 且取像素数据最多的那张，实际 %dx%d/%d 字节", got.width, got.height, len(got.data))
	}
	if got, err := selectIcon(images, 256); err != nil || got.width != 256 {
		t.Fatalf("精确匹配 256 失败: %+v %v", got, err)
	}
	if got, err := selectIcon(images, 8); err != nil || got.width != 16 {
		t.Fatalf("无精确匹配时应取最近尺寸 16: %+v %v", got, err)
	}
	if _, err := selectIcon(nil, 32); err == nil {
		t.Fatal("没有图像时应返回错误")
	}
}

// 结构体布局错了 Shell_NotifyIconW 会直接失败，这里锁住 x64 上的关键尺寸。
func TestWindowsStructSizes(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("仅在 64 位 Windows 上校验布局")
	}
	if got := unsafe.Sizeof(notifyIconData{}); got != 976 {
		t.Fatalf("NOTIFYICONDATAW 大小 = %d，期望 976", got)
	}
	if got := unsafe.Sizeof(wndClassEx{}); got != 80 {
		t.Fatalf("WNDCLASSEXW 大小 = %d，期望 80", got)
	}
	if got := unsafe.Sizeof(msg{}); got != 48 {
		t.Fatalf("MSG 大小 = %d，期望 48", got)
	}
	if got := unsafe.Sizeof(point{}); got != 8 {
		t.Fatalf("POINT 大小 = %d，期望 8", got)
	}
	// 只对总大小还不够：字段顺序错位同样会让 Shell_NotifyIconW 拒绝请求。
	offsets := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"notifyIconData.HWnd", unsafe.Offsetof(notifyIconData{}.HWnd), 8},
		{"notifyIconData.HIcon", unsafe.Offsetof(notifyIconData{}.HIcon), 32},
		{"notifyIconData.SzTip", unsafe.Offsetof(notifyIconData{}.SzTip), 40},
		{"notifyIconData.SzInfo", unsafe.Offsetof(notifyIconData{}.SzInfo), 304},
		{"notifyIconData.UVersion", unsafe.Offsetof(notifyIconData{}.UVersion), 816},
		{"notifyIconData.SzInfoTitle", unsafe.Offsetof(notifyIconData{}.SzInfoTitle), 820},
		{"notifyIconData.DwInfoFlags", unsafe.Offsetof(notifyIconData{}.DwInfoFlags), 948},
		{"notifyIconData.GuidItem", unsafe.Offsetof(notifyIconData{}.GuidItem), 952},
		{"notifyIconData.HBalloonIcon", unsafe.Offsetof(notifyIconData{}.HBalloonIcon), 968},
		{"wndClassEx.WndProc", unsafe.Offsetof(wndClassEx{}.WndProc), 8},
		{"wndClassEx.Instance", unsafe.Offsetof(wndClassEx{}.Instance), 24},
		{"wndClassEx.ClassName", unsafe.Offsetof(wndClassEx{}.ClassName), 64},
		{"wndClassEx.IconSm", unsafe.Offsetof(wndClassEx{}.IconSm), 72},
		{"msg.Message", unsafe.Offsetof(msg{}.Message), 8},
		{"msg.WParam", unsafe.Offsetof(msg{}.WParam), 16},
		{"msg.LParam", unsafe.Offsetof(msg{}.LParam), 24},
		{"msg.Pt", unsafe.Offsetof(msg{}.Pt), 36},
	}
	for _, o := range offsets {
		if o.got != o.want {
			t.Fatalf("%s 偏移 = %d，期望 %d", o.name, o.got, o.want)
		}
	}
}
