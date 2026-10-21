//go:build !windows

package dashboard

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"
)

// readKeys 在非 Windows 平台按行读取并映射首字符；ctx 取消或 stdin 结束时关闭通道。
func readKeys(ctx context.Context, keys chan<- byte) {
	defer close(keys)
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if line = strings.TrimSpace(line); line != "" {
			if !sendKeys(ctx, keys, []byte{line[0]}) {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// isTTY 报告 out 是否是字符设备（终端）；重定向到管道/文件时为 false。
func isTTY(out io.Writer) bool {
	f, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// enableVT 在非 Windows 平台的终端默认支持 ANSI 转义序列。
func enableVT() bool { return true }
