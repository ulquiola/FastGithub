package config

import (
	"path/filepath"
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
