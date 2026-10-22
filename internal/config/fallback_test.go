package config

import (
	"net"
	"strings"
	"testing"
)

func TestDefaultFallbackIPs(t *testing.T) {
	c := Default()
	for _, domain := range []string{"github.com", "api.github.com", "codeload.github.com"} {
		ips := c.FallbackIPs[domain]
		if len(ips) < 3 {
			t.Fatalf("%s 备用 IP 数量 = %d，期望至少 3 个", domain, len(ips))
		}
		for _, raw := range ips {
			if net.ParseIP(raw) == nil {
				t.Fatalf("%s 备用 IP 无法解析: %q", domain, raw)
			}
		}
	}
}

func TestDecodeFallbackIPs(t *testing.T) {
	c := Default()
	root, err := Parse([]byte(`
[fallback_ips]
"github.com" = ["1.2.3.4", "5.6.7.8"]
"api.github.com" = ["9.9.9.9"]
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := Decode(root, c); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := c.FallbackIPs["github.com"]; len(got) != 2 || got[0] != "1.2.3.4" || got[1] != "5.6.7.8" {
		t.Fatalf("github.com 备用 IP = %v", got)
	}
	if got := c.FallbackIPs["api.github.com"]; len(got) != 1 || got[0] != "9.9.9.9" {
		t.Fatalf("api.github.com 备用 IP = %v", got)
	}
	if len(c.FallbackIPs["codeload.github.com"]) == 0 {
		t.Fatal("未被覆盖的默认备用 IP 不应丢失")
	}
}

func TestValidateFallbackIPs(t *testing.T) {
	c := Default()
	c.FallbackIPs = map[string][]string{
		"github.com":   {"1.2.3.4", "bad"},
		"GitHub.com.":  {"1.2.3.4"},
		"*.github.com": {"1.2.3.4"},
		"":             {"1.2.3.4"},
	}
	err := c.Validate()
	if err == nil {
		t.Fatal("非法备用 IP 应校验失败")
	}
	for _, want := range []string{
		"fallback_ips.github.com[1]",
		"fallback_ips.*.github.com 只支持精确域名",
		"fallback_ips 的域名不能为空",
		"规范化后重复",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("错误 %q 缺少 %q", err, want)
		}
	}
}
