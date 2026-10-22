//go:build !darwin

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "FastGithub 菜单栏应用仅支持 macOS")
	os.Exit(1)
}
