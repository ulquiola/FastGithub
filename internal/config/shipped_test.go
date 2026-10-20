package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShippedConfig(t *testing.T) {
	root := filepath.Join("..", "..")
	cfg, err := Load(filepath.Join(root, "fastgithub.toml"), filepath.Join(root, "conf.d"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	st, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	d, ok := st.Lookup("github.com")
	if !ok {
		t.Fatal("github.com 未命中")
	}
	if d.TLSSNI {
		t.Fatal("github.com 不应发送 SNI")
	}
	if d.TLSIgnoreNameMismatch {
		t.Fatal("github.com 不应忽略证书名不匹配：精确规则必须优先于 *.github.com")
	}

	d, ok = st.Lookup("api.github.com")
	if !ok {
		t.Fatal("api.github.com 未命中")
	}
	if d.TLSIgnoreNameMismatch {
		t.Fatalf("api.github.com 应命中精确规则而非 *.github.com，got=%+v", d)
	}

	d, ok = st.Lookup("raw.githubusercontent.com")
	if !ok || !d.TLSIgnoreNameMismatch {
		t.Fatalf("raw.githubusercontent.com 应忽略证书名不匹配，got=%+v", d)
	}

	d, ok = st.Lookup("fonts.googleapis.com")
	if !ok || d.Destination != "https://fonts.geekzu.org/" {
		t.Fatalf("fonts.googleapis.com 配置错误: %+v", d)
	}

	if _, ok := st.Lookup("example.com"); ok {
		t.Fatal("example.com 不应命中")
	}
	if cfg.Server.ProxyPort != 38457 || cfg.Server.UIPort != 38470 {
		t.Fatalf("端口不符: %+v", cfg.Server)
	}
}

func TestShippedConfigExpanded(t *testing.T) {
	root := filepath.Join("..", "..")
	cfg, err := Load(filepath.Join(root, "fastgithub.toml"), filepath.Join(root, "conf.d"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	st, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	cases := []struct {
		domain string
		sni    bool
		ignore bool
		dest   string
	}{
		{"az764295.vo.msecnd.net", true, false, "https://vscode.cdn.azure.cn/"},
		{"onedrive.live.com", false, false, ""},
		{"sub.onedrive.live.com", false, false, ""},
		{"registry.nuget.org", true, false, ""},
		{"cdn.bootcss.com", true, false, "https://cdnjs.cloudflare.com/ajax/libs/"},
		{"v2ex.com", false, false, ""},
		{"s3.amazonaws.com", false, true, ""},
		{"foo.fastly.net", false, true, ""},
		{"imgur.com", false, true, ""},
	}
	for _, c := range cases {
		d, ok := st.Lookup(c.domain)
		if !ok {
			t.Fatalf("%s 未命中", c.domain)
		}
		if d.TLSSNI != c.sni || d.TLSIgnoreNameMismatch != c.ignore || d.Destination != c.dest {
			t.Fatalf("%s = %+v，期望 sni=%v ignore=%v dest=%q", c.domain, d, c.sni, c.ignore, c.dest)
		}
	}
}

func TestShippedConfigGolden(t *testing.T) {
	root := filepath.Join("..", "..")
	cfg, err := Load(filepath.Join(root, "fastgithub.toml"), filepath.Join(root, "conf.d"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "shipped_domains.txt"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(cfg.Domains) != len(lines) {
		t.Fatalf("域名规则数 = %d，期望 %d", len(cfg.Domains), len(lines))
	}
	for _, line := range lines {
		parts := strings.SplitN(line, "\t", 4)
		if len(parts) != 4 {
			t.Fatalf("golden 行格式错误: %q", line)
		}
		d, ok := cfg.Domains[parts[0]]
		if !ok {
			t.Fatalf("缺少规则 %s", parts[0])
		}
		if d.TLSSNI != (parts[1] == "true") || d.TLSIgnoreNameMismatch != (parts[2] == "true") || d.Destination != parts[3] {
			t.Fatalf("%s = %+v，golden = %q", parts[0], d, line)
		}
	}
}
