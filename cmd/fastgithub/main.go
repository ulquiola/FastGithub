package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"fastgithub-go/internal/app"
	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/intercept"
	"fastgithub-go/internal/tray"
)

const usage = `用法: fastgithub [命令]

不带命令时启动通知区域图标（托盘）。注册托盘需要管理员权限，会自动请求提权。

命令:
  tray [--path 文件]                   启动通知区域图标（需管理员）
  config validate [--path 文件]        校验配置
  install-cert [--dir 部署目录]        生成 CA 并加入信任库（CA 存于其下 cacert/；需管理员/root；macOS 非 root 时为用户级）
  uninstall-cert [--dir 部署目录]      从信任库移除 CA（需管理员/root）
  install                              安装系统服务（需管理员/root）
  uninstall                            卸载系统服务
  start                                启动系统服务
  stop                                 停止系统服务
  run [--path 文件]                    启动 38457 显式代理与 UI
  windivert-selftest                   打印 WinDivert 地址/DNS 自检
  version                              打印版本信息
`

// installCert / uninstallCert 可在测试中替换，避免真正修改系统信任库。
var installCert = certs.Install
var uninstallCert = certs.Uninstall

func main() {
	// Windows 服务由 SCM 拉起时先注册控制处理器；交互运行时该调用失败并继续。
	if handled, code := runAsService(); handled {
		os.Exit(code)
	}
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return runTrayReport(nil, stdout, stderr)
	}
	switch args[0] {
	case "-h", "--help", "help":
		if len(args) != 1 {
			return usageError(stderr)
		}
		fmt.Fprint(stdout, usage)
		return 0
	case "version":
		if len(args) != 1 {
			return usageError(stderr)
		}
		fmt.Fprintln(stdout, app.BuildInfo())
		return 0
	case "tray":
		return runTrayReport(args[1:], stdout, stderr)
	case "config":
		if err := runConfig(args[1:], stdout); err != nil {
			fmt.Fprintln(stderr, "错误: "+err.Error())
			return 1
		}
		return 0
	case "install-cert":
		if err := runInstallCert(args[1:], stdout); err != nil {
			fmt.Fprintln(stderr, "错误: "+err.Error())
			return 1
		}
		return 0
	case "uninstall-cert":
		if err := runUninstallCert(args[1:], stdout); err != nil {
			fmt.Fprintln(stderr, "错误: "+err.Error())
			return 1
		}
		return 0
	case "run":
		if err := runRun(args[1:], stdout); err != nil {
			fmt.Fprintln(stderr, "错误: "+err.Error())
			return 1
		}
		return 0
	case "windivert-selftest":
		if len(args) != 1 {
			return usageError(stderr)
		}
		fmt.Fprint(stdout, intercept.SelfTestText())
		return 0
	case "install", "uninstall", "start", "stop":
		if len(args) != 1 {
			return usageError(stderr)
		}
		if err := runServiceCommand(args[0]); err != nil {
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

// startTray 可在测试中替换，避免测试进程创建真实的通知区域图标。
var startTray = runTrayCommand

// acquireTrayInstance 可在测试中替换，避免测试进程占用真实的单实例互斥体。
var acquireTrayInstance = tray.AcquireSingleInstance

func runTrayReport(args []string, stdout, stderr io.Writer) int {
	if err := startTray(args, stdout, stderr); err != nil {
		if errors.Is(err, tray.ErrAlreadyRunning) {
			fmt.Fprintln(stdout, "已有一个 FastGithub 托盘实例在运行，本进程退出。")
			return 0
		}
		fmt.Fprintln(stderr, "错误: "+err.Error())
		return 1
	}
	return 0
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

func runInstallCert(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("install-cert", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", certs.DefaultDir, "部署目录（CA 位于其下 cacert/）")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("参数错误: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("用法: fastgithub install-cert [--dir 部署目录]")
	}
	a, err := certs.LoadOrCreate(certs.CACertDir(*dir))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "CA 证书: %s\n", a.CertPath())
	if err := installCert(a.CertPath()); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "已加入信任库")
	return nil
}

func runUninstallCert(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("uninstall-cert", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dir := fs.String("dir", certs.DefaultDir, "部署目录（CA 位于其下 cacert/）")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("参数错误: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("用法: fastgithub uninstall-cert [--dir 部署目录]")
	}
	certPath, err := certs.CertPath(certs.CACertDir(*dir))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "CA 证书: %s\n", certPath)
	if err := uninstallCert(certPath); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "已从信任库移除")
	return nil
}

func runServiceCommand(command string) error {
	switch command {
	case "install":
		exe, err := os.Executable()
		if err != nil {
			return fmt.Errorf("定位可执行文件失败: %w", err)
		}
		return app.InstallService(exe)
	case "uninstall":
		return app.UninstallService()
	case "start":
		return app.StartService()
	case "stop":
		return app.StopService()
	default:
		return fmt.Errorf("未知服务命令 %s", command)
	}
}

func runRun(args []string, stdout io.Writer) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runRunWithContext(ctx, args, stdout)
}

func runRunWithContext(ctx context.Context, args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("path", "", "配置文件路径")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("参数错误: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("用法: fastgithub run [--path 文件]")
	}
	cfg, store, _, err := loadConfig(*path)
	if err != nil {
		return err
	}
	return app.Run(ctx, cfg, store, func(proxy, ui net.Addr) {
		fmt.Fprintf(stdout, "代理已启动: %s\nUI: http://%s/\n", proxy, ui)
	})
}

// loadConfig 发现并加载配置与规则库，然后切到配置目录。
// 服务以绝对路径启动时，让 cacert/ui.token/logs 落在配置目录旁。
func loadConfig(path string) (*config.Config, *config.Store, string, error) {
	mainPath, confDir, err := config.Discover(path)
	if err != nil {
		return nil, nil, "", err
	}
	cfg, err := config.Load(mainPath, confDir)
	if err != nil {
		return nil, nil, "", err
	}
	store, err := config.NewStore(cfg)
	if err != nil {
		return nil, nil, "", err
	}
	if err := os.Chdir(filepath.Dir(mainPath)); err != nil {
		return nil, nil, "", fmt.Errorf("切换到配置目录失败: %w", err)
	}
	return cfg, store, mainPath, nil
}

// addresses 是代理与 UI 的实际监听地址。
type addresses struct{ proxy, ui net.Addr }

// runTrayCommand 加载配置后以托盘模式常驻；托盘起不来就报错（不降级）。
func runTrayCommand(args []string, stdout, _ io.Writer) error {
	fs := flag.NewFlagSet("tray", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("path", "", "配置文件路径")
	keepConsole := fs.Bool("console", false, "保留控制台窗口（调试用）")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("参数错误: %w", err)
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("用法: fastgithub tray [--path 文件]")
	}
	// 注册通知区域图标需要与 Explorer 同级或更高权限；本程序本身也需要管理员
	// （WinDivert、监听 443、写入信任库），所以托盘模式统一以管理员身份运行。
	if runtime.GOOS == "windows" && !tray.IsAdmin() {
		if err := tray.RelaunchElevated(append([]string{"tray"}, args...)); err != nil {
			return err
		}
		return nil
	}
	cfg, store, configPath, err := loadConfig(*path)
	if err != nil {
		return err
	}

	// 在启动代理之前抢占单实例：第二个实例必须在这里退出，不能先绑定端口再失败。
	release, err := acquireTrayInstance()
	if err != nil {
		if errors.Is(err, tray.ErrAlreadyRunning) {
			fmt.Fprintln(stdout, "已有一个 FastGithub 托盘实例在运行，本进程退出。")
			return nil
		}
		return err
	}
	defer release()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return serveWithUI(ctx, cancel, cfg, store, func(addr addresses) error {
		admin := tray.IsAdmin()
		autostart, _ := tray.AutostartEnabled()
		fmt.Fprintf(stdout, "代理已启动: %s\nUI: http://%s/\n", addr.proxy, addr.ui)
		opts := tray.Options{
			Version:    app.Version(),
			ProxyAddr:  addr.proxy.String(),
			UIAddr:     addr.ui.String(),
			ConfigPath: configPath,
			// 透明拦截是否真正可用取决于 WinDivert，这里只能按权限与配置推断。
			InterceptEnabled: admin && cfg.Intercept.Mode != "off",
			Admin:            admin,
			Autostart:        autostart,
			Actions:          tray.Actions{Quit: cancel},
		}
		// 托盘接管后不再需要控制台窗口；--console 可保留用于排查。
		if !*keepConsole {
			detachConsole()
		}
		return tray.Run(ctx, opts)
	})
}

// serveWithUI 在后台跑 app.Run，把实际监听地址交给 runUI（托盘）；
// runUI 返回或 ctx 取消后关闭代理并等待 app.Run 返回。
func serveWithUI(ctx context.Context, cancel context.CancelFunc, cfg *config.Config, store *config.Store, runUI func(addresses) error) error {
	readyCh := make(chan addresses, 1)
	done := make(chan error, 1)
	go func() {
		err := app.Run(ctx, cfg, store, func(proxy, ui net.Addr) {
			readyCh <- addresses{proxy: proxy, ui: ui}
		})
		done <- err
		// 代理意外结束时让 UI 也退出，避免留下没有代理的图标/面板。
		cancel()
	}()

	var addr addresses
	select {
	case addr = <-readyCh:
	case err := <-done:
		return err
	case <-ctx.Done():
		return waitApp(done)
	}

	uiErr := runUI(addr)
	cancel()
	if appErr := waitApp(done); uiErr == nil {
		uiErr = appErr
	}
	return uiErr
}

// waitApp 等待 app.Run 退出；托盘退出不应无限阻塞进程。
func waitApp(done <-chan error) error {
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		return errors.New("等待代理退出超时")
	}
}
