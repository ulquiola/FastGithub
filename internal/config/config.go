package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Config 对应 fastgithub.toml 的根表。
type Config struct {
	Server      Server              `toml:"server"`
	DNS         DNS                 `toml:"dns"`
	Intercept   Intercept           `toml:"intercept"`
	Log         Log                 `toml:"log"`
	Domains     map[string]*Domain  `toml:"domains"`
	FallbackIPs map[string][]string `toml:"fallback_ips"`
}

// Server 是监听相关配置。
type Server struct {
	ProxyPort   int         `toml:"proxy_port"`
	UIPort      int         `toml:"ui_port"`
	Singleton   bool        `toml:"singleton"`
	HTTP2       bool        `toml:"http2"`
	Transparent Transparent `toml:"transparent"`
}

// Transparent 是 Windows 透明入口的偏好端口。
type Transparent struct {
	HTTPS int `toml:"https"`
	HTTP  int `toml:"http"`
	SSH   int `toml:"ssh"`
	Git   int `toml:"git"`
}

// DNS 是域名解析配置。
type DNS struct {
	DoH      []string `toml:"doh"`
	Fallback []string `toml:"fallback"`
	Timeout  Duration `toml:"timeout"`
}

// Intercept 是平台劫持配置。
type Intercept struct {
	Mode      string `toml:"mode"`
	DriverDir string `toml:"driver_dir"`
}

// Log 是日志配置。
type Log struct {
	Level string `toml:"level"`
	File  string `toml:"file"`
}

// Domain 是单个域名规则的配置。
type Domain struct {
	TLSSNI                bool      `toml:"tls_sni"`
	TLSSNIPattern         string    `toml:"tls_sni_pattern"`
	TLSIgnoreNameMismatch bool      `toml:"tls_ignore_name_mismatch"`
	Timeout               Duration  `toml:"timeout"`
	IP                    string    `toml:"ip"`
	Destination           string    `toml:"destination"`
	Response              *Response `toml:"response"`

	pattern *Pattern
	dest    *url.URL
	ipAddr  net.IP
}

// Response 是短路响应的配置。
type Response struct {
	Status      int    `toml:"status"`
	ContentType string `toml:"content_type"`
	Content     string `toml:"content"`
}

// Default 返回与设计文档一致的内置默认值。
func Default() *Config {
	return &Config{
		Server: Server{
			ProxyPort: 38457,
			UIPort:    38470,
			Singleton: true,
			HTTP2:     true,
			Transparent: Transparent{
				HTTPS: 443,
				HTTP:  80,
				SSH:   22,
				Git:   9418,
			},
		},
		DNS: DNS{
			DoH: []string{
				"https://doh.pub/dns-query",
				"https://dns.alidns.com/dns-query",
			},
			Fallback: []string{
				"8.8.8.8:53",
				"119.29.29.29:53",
				"114.114.114.114:53",
			},
			Timeout: Duration(4 * time.Second),
		},
		Intercept: Intercept{Mode: "auto"},
		Log:       Log{Level: "info", File: "logs/fastgithub.log"},
		Domains:   map[string]*Domain{},
		// DNS 只返回单个区域 IP 时，答案不可达会让整个域名 502。
		// 这些备用 IP 与 DNS 答案一起竞速，只在 TCP 可达时才会被选用。
		FallbackIPs: map[string][]string{
			"github.com": {
				"140.82.113.3", "140.82.114.3", "140.82.116.3",
				"20.27.177.113", "20.200.245.247",
			},
			"api.github.com": {
				"140.82.113.6", "140.82.114.6", "140.82.116.6",
			},
			"codeload.github.com": {
				"140.82.113.9", "140.82.114.9", "140.82.116.9",
			},
		},
	}
}

// Validate 校验字段取值范围与格式，返回聚合错误。
func (c *Config) Validate() error {
	var errs []string
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Sprintf(format, args...))
	}

	if c.Server.ProxyPort < 1 || c.Server.ProxyPort > 65535 {
		add("server.proxy_port 必须在 1~65535 之间")
	}
	if c.Server.UIPort < 1 || c.Server.UIPort > 65535 {
		add("server.ui_port 必须在 1~65535 之间")
	}
	if c.Server.ProxyPort == c.Server.UIPort {
		add("server.ui_port 不能与 server.proxy_port 相同")
	}

	ports := []struct {
		name string
		port int
	}{
		{"https", c.Server.Transparent.HTTPS},
		{"http", c.Server.Transparent.HTTP},
		{"ssh", c.Server.Transparent.SSH},
		{"git", c.Server.Transparent.Git},
	}
	seen := map[int]string{}
	for _, p := range ports {
		if p.port < 1 || p.port > 65535 {
			add("server.transparent.%s 必须在 1~65535 之间", p.name)
			continue
		}
		if other, ok := seen[p.port]; ok {
			add("server.transparent.%s 与 %s 端口重复", p.name, other)
		}
		seen[p.port] = p.name
	}

	if c.Intercept.Mode != "auto" && c.Intercept.Mode != "off" {
		add("intercept.mode 只能是 auto 或 off")
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		add("log.level 只能是 debug/info/warn/error")
	}

	if len(c.DNS.DoH) == 0 && len(c.DNS.Fallback) == 0 {
		add("dns.doh 与 dns.fallback 不能同时为空")
	}
	for i, raw := range c.DNS.DoH {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			add("dns.doh[%d] 必须是 https URL", i)
		}
	}
	for i, raw := range c.DNS.Fallback {
		host, port, err := net.SplitHostPort(raw)
		if err != nil || host == "" || port == "" {
			add("dns.fallback[%d] 必须是 host:port", i)
		}
	}
	if c.DNS.Timeout.Std() <= 0 {
		add("dns.timeout 必须大于 0")
	}

	names := make([]string, 0, len(c.Domains))
	for name := range c.Domains {
		names = append(names, name)
	}
	sort.Strings(names)
	seenRules := make(map[string]string, len(c.Domains))
	for _, name := range names {
		if name == "" {
			continue
		}
		// NewPattern 会统一小写，大小写不同的等价表达式在 Compare 下相等，
		// 初始顺序又取决于 map 迭代，匹配优先级不确定，因此直接拒绝。
		lower := strings.ToLower(name)
		if first, ok := seenRules[lower]; ok {
			add("域名规则 %q 与 %q 忽略大小写后重复", first, name)
			continue
		}
		seenRules[lower] = name
	}
	for _, name := range names {
		d := c.Domains[name]
		if name == "" {
			add("域名规则不能为空")
			continue
		}
		if _, err := NewPattern(name); err != nil {
			add("域名规则 %q: %v", name, err)
			continue
		}
		if d == nil {
			add("域名规则 %q 的配置为空", name)
			continue
		}
		if d.IP != "" && net.ParseIP(d.IP) == nil {
			add("域名规则 %q 的 ip 无法解析: %s", name, d.IP)
		}
		if d.Destination != "" {
			if _, err := url.Parse(d.Destination); err != nil {
				add("域名规则 %q 的 destination 无法解析: %v", name, err)
			}
		}
		if d.Response != nil {
			if d.Response.Status < 100 || d.Response.Status > 599 {
				add("域名规则 %q 的 response.status 必须在 100~599 之间", name)
			}
			if d.Response.Content != "" && d.Response.ContentType == "" {
				add("域名规则 %q 有 response.content 时必须给出 response.content_type", name)
			}
		}
	}

	fallbackNames := make([]string, 0, len(c.FallbackIPs))
	for name := range c.FallbackIPs {
		fallbackNames = append(fallbackNames, name)
	}
	sort.Strings(fallbackNames)
	seenFallback := make(map[string]string, len(c.FallbackIPs))
	for _, name := range fallbackNames {
		if name == "" {
			add("fallback_ips 的域名不能为空")
			continue
		}
		key := normalizeFallbackHost(name)
		if first, ok := seenFallback[key]; ok {
			add("fallback_ips 的域名 %q 与 %q 规范化后重复", first, name)
		} else {
			seenFallback[key] = name
		}
		if strings.Contains(name, "*") {
			add("fallback_ips.%s 只支持精确域名", name)
			continue
		}
		if _, err := NewPattern(name); err != nil {
			add("fallback_ips.%s: %v", name, err)
			continue
		}
		ips := c.FallbackIPs[name]
		if len(ips) == 0 {
			add("fallback_ips.%s 不能为空", name)
			continue
		}
		for i, raw := range ips {
			if net.ParseIP(raw) == nil {
				add("fallback_ips.%s[%d] 无法解析: %s", name, i, raw)
			}
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
