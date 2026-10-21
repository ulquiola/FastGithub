//go:build windows

package tray

import (
	_ "embed"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

//go:embed assets/app.ico
var appIconData []byte

const (
	iconDirSize   = 6
	iconEntrySize = 16
	iconMaxSize   = 256
)

// iconImage 是 .ico 中的一张图像；width/height 已把 ICO 的 0 换算成 256。
type iconImage struct {
	width  int
	height int
	data   []byte
}

// parseICO 解析 ICONDIR 并返回全部图像；数据非法时返回错误而不是 panic。
func parseICO(data []byte) ([]iconImage, error) {
	if len(data) < iconDirSize {
		return nil, fmt.Errorf("ICO 数据过短: %d 字节", len(data))
	}
	if reserved := binary.LittleEndian.Uint16(data[0:]); reserved != 0 {
		return nil, fmt.Errorf("ICO 保留字段应为 0，实际 %d", reserved)
	}
	if typ := binary.LittleEndian.Uint16(data[2:]); typ != 1 {
		return nil, fmt.Errorf("不是图标文件（类型 %d）", typ)
	}
	count := int(binary.LittleEndian.Uint16(data[4:]))
	if count == 0 {
		return nil, errors.New("ICO 不含任何图像")
	}
	if len(data) < iconDirSize+count*iconEntrySize {
		return nil, fmt.Errorf("ICO 目录被截断：需要 %d 字节，实际 %d",
			iconDirSize+count*iconEntrySize, len(data))
	}

	images := make([]iconImage, 0, count)
	for i := 0; i < count; i++ {
		entry := data[iconDirSize+i*iconEntrySize:]
		width, height := int(entry[0]), int(entry[1])
		if width == 0 {
			width = iconMaxSize
		}
		if height == 0 {
			height = iconMaxSize
		}
		size := int(binary.LittleEndian.Uint32(entry[8:]))
		offset := int(binary.LittleEndian.Uint32(entry[12:]))
		if size <= 0 || offset < 0 || size > len(data) || offset > len(data)-size {
			return nil, fmt.Errorf("ICO 第 %d 项越界：偏移 %d，大小 %d，总共 %d 字节",
				i, offset, size, len(data))
		}
		images = append(images, iconImage{
			width:  width,
			height: height,
			data:   data[offset : offset+size],
		})
	}
	return images, nil
}

// selectIcon 选出最合适的一张：优先精确尺寸，精确尺寸中取像素数据最多的
// （同样大小下字节越多通常色深越高）；没有精确尺寸时取尺寸最接近的。
func selectIcon(images []iconImage, want int) (iconImage, error) {
	if len(images) == 0 {
		return iconImage{}, errors.New("没有可用的图标图像")
	}
	var best iconImage
	bestDiff := int(^uint(0) >> 1)
	for _, img := range images {
		diff := img.width - want
		if diff < 0 {
			diff = -diff
		}
		if diff > bestDiff {
			continue
		}
		if diff < bestDiff ||
			img.width > best.width ||
			(img.width == best.width && len(img.data) > len(best.data)) {
			best, bestDiff = img, diff
		}
	}
	return best, nil
}

// loadAppIcon 从内嵌 .ico 创建 HICON；失败时退回系统默认应用图标。
// owned 为 false 表示返回的是 LoadIconW 的共享图标，不能 DestroyIcon。
func loadAppIcon() (hicon syscall.Handle, owned bool) {
	if images, err := parseICO(appIconData); err == nil {
		if img, err := selectIcon(images, 32); err == nil {
			if hicon, err := createIcon(img); err == nil {
				return hicon, true
			}
		}
	}
	h, _, _ := procLoadIconW.Call(0, idiApplication)
	return syscall.Handle(h), false
}

// createIcon 用 CreateIconFromResourceEx 把单张 ICO 图像转成 HICON。
func createIcon(img iconImage) (syscall.Handle, error) {
	if len(img.data) == 0 {
		return 0, errors.New("图标数据为空")
	}
	h, _, errno := procCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&img.data[0])),
		uintptr(len(img.data)),
		1, // fIcon
		0x00030000,
		uintptr(img.width),
		uintptr(img.height),
		0, // LR_DEFAULTCOLOR
	)
	// img.data 的指针以 uintptr 传入，调用期间必须保证底层数组不被回收。
	runtime.KeepAlive(img.data)
	if h == 0 {
		return 0, fmt.Errorf("CreateIconFromResourceEx 失败: %w", errno)
	}
	return syscall.Handle(h), nil
}
