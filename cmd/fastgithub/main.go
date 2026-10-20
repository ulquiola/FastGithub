package main

import (
	"fmt"
	"io"
	"os"

	"fastgithub-go/internal/app"
)

const usage = `用法: fastgithub <命令>

命令:
  config validate [--path 文件]        校验配置
  version                              打印版本信息
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 1
	}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			fmt.Fprint(stderr, usage)
			return 1
		}
		fmt.Fprintln(stdout, app.BuildInfo())
		return 0
	default:
		fmt.Fprint(stderr, usage)
		return 1
	}
}
