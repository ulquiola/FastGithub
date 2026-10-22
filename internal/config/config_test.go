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

func TestDefaultHTTP2Enabled(t *testing.T) {
	if !Default().Server.HTTP2 {
		t.Fatal("默认应开启 HTTP/2")
	}
}

func TestDecodeHTTP2Override(t *testing.T) {
	c := Default()
	root, err := Parse([]byte("[server]\nhttp2 = false\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Decode(root, c); err != nil {
		t.Fatal(err)
	}
	if c.Server.HTTP2 {
		t.Fatal("http2 = false 应覆盖默认值")
	}
}

func TestDecodeDomainDNS(t *testing.T) {
	c := Default()
	root, err := Parse([]byte("[domains.\"linux.do\"]\ndns = \"https://uuu.ddd.oaifree.com/query-dns\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Decode(root, c); err != nil {
		t.Fatal(err)
	}
	got := c.Domains["linux.do"]
	if got == nil || got.DNS != "https://uuu.ddd.oaifree.com/query-dns" {
		t.Fatalf("domain = %+v", got)
	}
}

func TestValidateDomainDNS(t *testing.T) {
	c := Default()
	c.Domains = map[string]*Domain{"linux.do": {DNS: "http://insecure/query-dns"}}
	err := c.Validate()
	if err == nil || !strings.Contains(err.Error(), "dns 必须是 https URL") {
		t.Fatalf("err = %v", err)
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
timeout = "2s"
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
	got := c.Domains["*.githubusercontent.com"]
	if got == nil || !got.TLSIgnoreNameMismatch {
		t.Fatalf("*.githubusercontent.com = %+v，tls_ignore_name_mismatch 应为 true", got)
	}
	if got.Timeout.Std() != 2*time.Second {
		t.Fatalf("*.githubusercontent.com timeout = %v，期望 2s", got.Timeout.Std())
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

func TestValidateRejectsCaseInsensitiveDuplicateDomains(t *testing.T) {
	c := Default()
	c.Domains = map[string]*Domain{
		"GitHub.com": {TLSSNI: true},
		"github.com": {TLSSNI: false},
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("大小写不同但等价的域名规则应被拒绝")
	}
	for _, want := range []string{"GitHub.com", "github.com", "重复"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v，期望包含 %q", err, want)
		}
	}
}

func TestValidateBranches(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{
			name:   "nil_domain",
			mutate: func(c *Config) { c.Domains = map[string]*Domain{"nil.com": nil} },
			want:   `域名规则 "nil.com" 的配置为空`,
		},
		{
			name:   "bad_ip",
			mutate: func(c *Config) { c.Domains = map[string]*Domain{"bad.com": {IP: "999.999.999.999"}} },
			want:   "ip 无法解析",
		},
		{
			name:   "bad_destination",
			mutate: func(c *Config) { c.Domains = map[string]*Domain{"bad.com": {Destination: "://bad"}} },
			want:   "destination 无法解析",
		},
		{
			name:   "response_status",
			mutate: func(c *Config) { c.Domains = map[string]*Domain{"bad.com": {Response: &Response{Status: 600}}} },
			want:   "response.status",
		},
		{
			name: "response_content_without_type",
			mutate: func(c *Config) {
				c.Domains = map[string]*Domain{"bad.com": {Response: &Response{Status: 200, Content: "x"}}}
			},
			want: "response.content_type",
		},
		{
			name:   "transparent_duplicate_port",
			mutate: func(c *Config) { c.Server.Transparent.HTTP = c.Server.Transparent.HTTPS },
			want:   "server.transparent.http",
		},
		{
			name:   "dns_timeout_non_positive",
			mutate: func(c *Config) { c.DNS.Timeout = 0 },
			want:   "dns.timeout",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Default()
			tc.mutate(c)
			err := c.Validate()
			if err == nil {
				t.Fatal("期望校验失败")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v，期望包含 %q", err, tc.want)
			}
		})
	}
}
