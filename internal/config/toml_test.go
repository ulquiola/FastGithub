package config

import (
	"strings"
	"testing"
)

func TestParseScalars(t *testing.T) {
	root, err := Parse([]byte(`
# 注释
port = 38457
name = "fast\"github"  # 行尾注释
enabled = true
disabled = false
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := root.Table["port"].Int; got != 38457 {
		t.Fatalf("port = %d", got)
	}
	if got := root.Table["name"].Str; got != `fast"github` {
		t.Fatalf("name = %q", got)
	}
	if !root.Table["enabled"].Bool {
		t.Fatal("enabled 应为 true")
	}
	if root.Table["disabled"].Bool {
		t.Fatal("disabled 应为 false")
	}
	if got := root.Table["port"].Line; got != 3 {
		t.Fatalf("port 行号 = %d，期望 3", got)
	}
}

func TestParseRejectsBadValue(t *testing.T) {
	_, err := Parse([]byte("port = not-a-number\n"))
	if err == nil || !strings.Contains(err.Error(), "第 1 行") {
		t.Fatalf("err = %v，期望带行号的错误", err)
	}
}

func TestParseTables(t *testing.T) {
	root, err := Parse([]byte(`
[server]
port = 1

[server.transparent]
https = 443

[domains."*.github.com"]
tls_sni = false
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	server := root.Table["server"].Table
	if server["port"].Int != 1 {
		t.Fatalf("server.port = %d", server["port"].Int)
	}
	trans := server["transparent"].Table
	if trans["https"].Int != 443 {
		t.Fatalf("server.transparent.https = %d", trans["https"].Int)
	}
	domains := root.Table["domains"].Table
	d, ok := domains["*.github.com"]
	if !ok {
		t.Fatalf("缺少域名表，实际键: %v", keysOf(domains))
	}
	if d.Table["tls_sni"].Bool {
		t.Fatal("tls_sni 应为 false")
	}
}

func TestParseDottedKey(t *testing.T) {
	root, err := Parse([]byte("server.proxy_port = 40000\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if root.Table["server"].Table["proxy_port"].Int != 40000 {
		t.Fatal("点号键未展开")
	}
}

func keysOf(m map[string]*Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestParseRejectsBadTableHeader(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"[[server]]\n", "表数组"},
		{"[server\n", "缺少 ']'"},
		{"a = 1\n[a]\n", "已被定义为值"},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("Parse(%q) err = %v，期望包含 %q", c.src, err, c.want)
		}
	}
}

func TestParseRejectsEmptyKeySegment(t *testing.T) {
	for _, key := range []string{"", ".a", "..b", "a..b", "a. .b"} {
		if parts, err := parseKeyPath(key); err == nil {
			t.Fatalf("parseKeyPath(%q) = %#v，期望错误", key, parts)
		}
	}
}

func TestParseStripsBOM(t *testing.T) {
	root, err := Parse([]byte("\xEF\xBB\xBFport = 1\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	n, ok := root.Table["port"]
	if !ok {
		t.Fatalf("带 BOM 的首行未解析，实际键: %v", keysOf(root.Table))
	}
	if n.Int != 1 {
		t.Fatalf("port = %d", n.Int)
	}
}
