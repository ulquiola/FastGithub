package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"fastgithub-go/internal/app"
	"fastgithub-go/internal/config"
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
		return usageError(stderr)
	}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			return usageError(stderr)
		}
		fmt.Fprintln(stdout, app.BuildInfo())
		return 0
	case "config":
		if err := runConfig(args[1:], stdout); err != nil {
			fmt.Fprintln(stderr, "错误: "+err.Error())
			return 1
		}
		return 0
	default:
		return usageError(stderr)
	}
}

// usageError 是所有顶层用法错误的唯一出口：打印用法并以 1 退出。
func usageError(stderr io.Writer) int {
	fmt.Fprint(stderr, usage)
	return 1
}

func runConfig(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "validate" {
		return fmt.Errorf("用法: fastgithub config validate [--path 文件]")
	}
	fs := flag.NewFlagSet("config validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("path", "", "配置文件路径")
	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("参数错误: %w", err)
	}

	mainPath, confDir, err := config.Discover(*path)
	if err != nil {
		return err
	}
	cfg, err := config.Load(mainPath, confDir)
	if err != nil {
		return err
	}
	store, err := config.NewStore(cfg)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "配置有效: %d 条域名规则，代理端口 %d，UI 端口 %d\n",
		len(store.Patterns()), cfg.Server.ProxyPort, cfg.Server.UIPort)
	return nil
}
