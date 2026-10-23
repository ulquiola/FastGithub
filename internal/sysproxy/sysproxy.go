package sysproxy

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"fastgithub-go/internal/procattr"
)

// ErrUnsupported 表示当前平台不支持自动配置系统代理。
var ErrUnsupported = errors.New("当前平台不支持自动配置系统代理")

// Status 描述系统代理的当前状态。
type Status struct {
	Supported bool   `json:"supported"`
	Enabled   bool   `json:"enabled"`
	Service   string `json:"service,omitempty"`
	Server    string `json:"server,omitempty"`
	Port      int    `json:"port,omitempty"`
	Note      string `json:"note,omitempty"`
}

type runner func(name string, args ...string) ([]byte, error)

// Get 检测系统代理是否指向 127.0.0.1:port。
func Get(port int) (Status, error) {
	return get(port, runtime.GOOS, run)
}

// Enable 把系统代理指向 127.0.0.1:port。
func Enable(port int) error {
	return enable(port, runtime.GOOS, run)
}

// Disable 关闭由 Enable 打开的系统代理；指向其他代理时不做修改。
func Disable(port int) error {
	return disable(port, runtime.GOOS, run)
}

func run(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	procattr.Hide(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func get(port int, goos string, run runner) (Status, error) {
	if goos != "darwin" {
		// 不支持的平台不是错误：UI 需要区分“不可用”和“检测失败”。
		return Status{Supported: false}, nil
	}
	service, err := primaryService(run)
	if err != nil {
		var tunnel *tunnelRouteError
		if errors.As(err, &tunnel) {
			// VPN 的 utun 接口没有对应的网络服务，无法用 networksetup 设置。
			return Status{Supported: false, Note: tunnelHint(tunnel.device, port)}, nil
		}
		return Status{}, err
	}
	http, err := proxyConfig(run, "-getwebproxy", service)
	if err != nil {
		return Status{}, err
	}
	https, err := proxyConfig(run, "-getsecurewebproxy", service)
	if err != nil {
		return Status{}, err
	}
	enabled := http.enabled && https.enabled &&
		http.server == "127.0.0.1" && http.port == port &&
		https.server == "127.0.0.1" && https.port == port
	return Status{
		Supported: true,
		Enabled:   enabled,
		Service:   service,
		Server:    http.server,
		Port:      http.port,
	}, nil
}

func enable(port int, goos string, run runner) error {
	if goos != "darwin" {
		return ErrUnsupported
	}
	service, err := primaryService(run)
	if err != nil {
		return tunnelError(err, port)
	}
	addr := strconv.Itoa(port)
	for _, args := range [][]string{
		{"-setwebproxy", service, "127.0.0.1", addr},
		{"-setsecurewebproxy", service, "127.0.0.1", addr},
	} {
		if _, err := run("networksetup", args...); err != nil {
			return fmt.Errorf("设置系统代理失败: %w", err)
		}
	}
	return nil
}

func disable(port int, goos string, run runner) error {
	if goos != "darwin" {
		return ErrUnsupported
	}
	service, err := primaryService(run)
	if err != nil {
		return tunnelError(err, port)
	}
	cfg, err := proxyConfig(run, "-getwebproxy", service)
	if err != nil {
		return err
	}
	if cfg.enabled && (cfg.server != "127.0.0.1" || cfg.port != port) {
		return fmt.Errorf("当前系统代理指向 %s:%d，不是 FastGithub，未做修改", cfg.server, cfg.port)
	}
	for _, args := range [][]string{
		{"-setwebproxystate", service, "off"},
		{"-setsecurewebproxystate", service, "off"},
	} {
		if _, err := run("networksetup", args...); err != nil {
			return fmt.Errorf("关闭系统代理失败: %w", err)
		}
	}
	return nil
}

type proxyConfigValues struct {
	enabled bool
	server  string
	port    int
}

func proxyConfig(run runner, flag, service string) (proxyConfigValues, error) {
	out, err := run("networksetup", flag, service)
	if err != nil {
		return proxyConfigValues{}, fmt.Errorf("读取系统代理失败: %w", err)
	}
	return parseProxyConfig(string(out)), nil
}

func parseProxyConfig(out string) proxyConfigValues {
	var cfg proxyConfigValues
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "Enabled":
			cfg.enabled = strings.EqualFold(value, "Yes")
		case "Server":
			cfg.server = value
		case "Port":
			cfg.port, _ = strconv.Atoi(value)
		}
	}
	return cfg
}

// primaryService 找到默认路由接口对应的网络服务名，例如 Wi-Fi。
func primaryService(run runner) (string, error) {
	out, err := run("route", "-n", "get", "default")
	if err != nil {
		return "", fmt.Errorf("读取默认路由失败: %w", err)
	}
	device := routeInterface(string(out))
	if device == "" {
		return "", errors.New("无法确定默认网络接口")
	}
	out, err = run("networksetup", "-listnetworkserviceorder")
	if err != nil {
		return "", fmt.Errorf("读取网络服务列表失败: %w", err)
	}
	if name := serviceForDevice(string(out), device); name != "" {
		return name, nil
	}
	return "", &tunnelRouteError{device: device}
}

type tunnelRouteError struct{ device string }

func (e *tunnelRouteError) Error() string {
	return "默认路由为 VPN/隧道接口 " + e.device
}

// tunnelError 把隧道路由错误转成带操作指引的错误。
func tunnelError(err error, port int) error {
	var tunnel *tunnelRouteError
	if errors.As(err, &tunnel) {
		return errors.New(tunnelHint(tunnel.device, port))
	}
	return err
}

func tunnelHint(device string, port int) string {
	return fmt.Sprintf("默认路由为 VPN/隧道接口 %s；请在 VPN 软件中把 HTTP/HTTPS 代理设为 127.0.0.1:%d", device, port)
}

func routeInterface(out string) string {
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if ok && strings.TrimSpace(key) == "interface" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func serviceForDevice(out, device string) string {
	lines := strings.Split(out, "\n")
	needle := "Device: " + device + ")"
	for i, line := range lines {
		if !strings.Contains(line, needle) {
			continue
		}
		for j := i - 1; j >= 0; j-- {
			name := strings.TrimSpace(lines[j])
			if name == "" {
				continue
			}
			if idx := strings.Index(name, ") "); strings.HasPrefix(name, "(") && idx >= 0 {
				return strings.TrimSpace(name[idx+2:])
			}
			break
		}
	}
	return ""
}
