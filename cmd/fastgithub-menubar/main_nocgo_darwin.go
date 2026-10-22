//go:build darwin && !cgo

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "macOS 菜单栏应用需要 CGO_ENABLED=1 构建")
	os.Exit(1)
}
