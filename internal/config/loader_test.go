package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoadMergesConfD(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fastgithub.toml"), "[server]\nproxy_port = 40000\n")
	write(t, filepath.Join(dir, "conf.d", "10-a.toml"), "[domains.\"a.com\"]\ntls_sni = true\n")
	write(t, filepath.Join(dir, "conf.d", "20-b.toml"), "[server]\nui_port = 40001\n")

	cfg, err := Load(filepath.Join(dir, "fastgithub.toml"), filepath.Join(dir, "conf.d"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ProxyPort != 40000 || cfg.Server.UIPort != 40001 {
		t.Fatalf("server = %+v", cfg.Server)
	}
	if _, ok := cfg.Domains["a.com"]; !ok {
		t.Fatalf("domains = %v", cfg.Domains)
	}
	if len(cfg.DNS.Fallback) != 3 {
		t.Fatalf("默认值未生效: %v", cfg.DNS.Fallback)
	}
}

func TestLoadConfDMergesInNameOrder(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fastgithub.toml"), "[server]\nproxy_port = 40000\n")
	write(t, filepath.Join(dir, "conf.d", "10-a.toml"), "[server]\nproxy_port = 41000\n")
	write(t, filepath.Join(dir, "conf.d", "20-b.toml"), "[server]\nproxy_port = 42000\n")

	cfg, err := Load(filepath.Join(dir, "fastgithub.toml"), filepath.Join(dir, "conf.d"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ProxyPort != 42000 {
		t.Fatalf("proxy_port = %d，期望按文件名升序后者覆盖", cfg.Server.ProxyPort)
	}
}

func TestLoadMissingMainFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(filepath.Join(dir, "nope.toml"), filepath.Join(dir, "conf.d")); err == nil {
		t.Fatal("主配置缺失应报错")
	}
}

func TestLoadReportsFileAndLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fastgithub.toml")
	write(t, path, "[server]\nproxy_port = oops\n")
	_, err := Load(path, "")
	if err == nil {
		t.Fatal("期望报错")
	}
	if got := err.Error(); !contains(got, "fastgithub.toml") || !contains(got, "第 2 行") {
		t.Fatalf("err = %v，期望包含文件名与行号", got)
	}
}

func TestDiscoverExplicit(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "fastgithub.toml")
	gotMain, gotDir, err := Discover(main)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if gotMain != main {
		t.Fatalf("mainPath = %q, want %q", gotMain, main)
	}
	if want := filepath.Join(dir, "conf.d"); gotDir != want {
		t.Fatalf("confDir = %q, want %q", gotDir, want)
	}
}

func TestFingerprintChanges(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "fastgithub.toml")
	write(t, main, "[server]\nproxy_port = 40000\n")
	fp1, err := Fingerprint(main, "")
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if fp1 == "" {
		t.Fatal("指纹不能为空")
	}
	write(t, main, "[server]\nproxy_port = 40001\n")
	fp2, err := Fingerprint(main, "")
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if fp1 == fp2 {
		t.Fatal("内容变化后指纹应变化")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
