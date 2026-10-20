package config

import (
	"strings"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.Server.ProxyPort != 38457 || c.Server.UIPort != 38470 {
		t.Fatalf("端口默认值错误: %+v", c.Server)
	}
	if c.Server.Transparent.HTTPS != 443 || c.Server.Transparent.Git != 9418 {
		t.Fatalf("透明端口默认值错误: %+v", c.Server.Transparent)
	}
	if len(c.DNS.Fallback) != 3 || len(c.DNS.DoH) != 2 {
		t.Fatalf("DNS 默认值错误: %+v", c.DNS)
	}
	if c.DNS.Timeout.Std() != 4*time.Second {
		t.Fatalf("DNS 超时 = %v", c.DNS.Timeout.Std())
	}
	if c.Intercept.Mode != "auto" {
		t.Fatalf("intercept.mode = %q", c.Intercept.Mode)
	}
	if c.Log.Level != "info" || c.Log.File != "logs/fastgithub.log" {
		t.Fatalf("log 默认值错误: %+v", c.Log)
	}
}

func TestDecodeIntoConfig(t *testing.T) {
	c := Default()
	root, err := Parse([]byte(`
[server]
proxy_port = 40000

[dns]
timeout = "1500ms"

[domains."github.com"]
tls_sni = false

[domains."*.githubusercontent.com"]
tls_ignore_name_mismatch = true
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := Decode(root, c); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if c.Server.ProxyPort != 40000 || c.Server.UIPort != 38470 {
		t.Fatalf("server = %+v", c.Server)
	}
	if c.DNS.Timeout.Std() != 1500*time.Millisecond {
		t.Fatalf("timeout = %v", c.DNS.Timeout.Std())
	}
	if len(c.Domains) != 2 || c.Domains["github.com"].TLSSNI {
		t.Fatalf("domains = %+v", c.Domains)
	}
}

func TestValidateErrors(t *testing.T) {
	c := Default()
	c.Server.ProxyPort = 0
	c.Server.UIPort = 0
	c.Intercept.Mode = "maybe"
	c.Log.Level = "verbose"
	c.DNS.DoH = []string{"http://insecure/dns-query"}
	c.DNS.Fallback = []string{"8.8.8.8"}
	c.Domains = map[string]*Domain{"": {}}
	err := c.Validate()
	if err == nil {
		t.Fatal("期望校验失败")
	}
	for _, want := range []string{"server.proxy_port", "intercept.mode", "log.level", "dns.doh", "dns.fallback", "域名规则"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v，缺少 %q", err, want)
		}
	}
}

func TestValidateOK(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("默认配置应通过校验: %v", err)
	}
}
