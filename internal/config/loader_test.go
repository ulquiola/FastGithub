package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

// Discover 内的 stat 以参数注入：Windows 上无法在不改 ACL 的前提下构造
// “本地配置存在但 stat 失败”，这里用真实错误类型覆盖该分支。
func TestDiscoverKeepsLocalConfigOnNonNotExistError(t *testing.T) {
	cases := []struct {
		name    string
		statErr error
	}{
		{"权限不足", fs.ErrPermission},
		{"IO 错误", errors.New("设备未就绪")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			main, confDir, err := discover("", func(string) (fs.FileInfo, error) {
				return nil, &fs.PathError{Op: "stat", Path: "fastgithub.toml", Err: tc.statErr}
			})
			if err != nil {
				t.Fatalf("discover: %v", err)
			}
			wantMain, err := filepath.Abs("fastgithub.toml")
			if err != nil {
				t.Fatalf("Abs: %v", err)
			}
			if main != wantMain {
				t.Fatalf("mainPath = %q，非 NotExist 的 stat 错误不应静默改用用户配置目录，期望 %q", main, wantMain)
			}
			if wantDir := filepath.Join(filepath.Dir(wantMain), "conf.d"); confDir != wantDir {
				t.Fatalf("confDir = %q, want %q", confDir, wantDir)
			}
		})
	}
}

func TestDiscoverFallsBackToUserConfigWhenNoLocal(t *testing.T) {
	main, confDir, err := discover("", func(string) (fs.FileInfo, error) {
		return nil, &fs.PathError{Op: "stat", Path: "fastgithub.toml", Err: fs.ErrNotExist}
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("本环境无法定位用户配置目录: %v", err)
	}
	if want := filepath.Join(base, "fastgithub", "fastgithub.toml"); main != want {
		t.Fatalf("mainPath = %q, want %q", main, want)
	}
	if want := filepath.Join(base, "fastgithub", "conf.d"); confDir != want {
		t.Fatalf("confDir = %q, want %q", confDir, want)
	}
}

func TestFingerprintWrapsConfDirError(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("非 Windows 下“<”不是非法路径字符，无法触发非 NotExist 的 ReadDir 错误")
	}
	dir := t.TempDir()
	main := filepath.Join(dir, "fastgithub.toml")
	write(t, main, "[server]\nproxy_port = 40000\n")
	// Windows 下 “<” 是非法路径字符，ReadDir 返回 ERROR_INVALID_NAME（非 NotExist），
	// 用它覆盖 conf.d 读取失败时的中文包装；权限类失败需改 ACL，测试里不做。
	badConf := filepath.Join(dir, "bad<name")

	_, err := Fingerprint(main, badConf)
	if err == nil {
		t.Fatal("conf.d 读取失败时应报错")
	}
	if got := err.Error(); !contains(got, "读取") || !contains(got, badConf) {
		t.Fatalf("err = %v，期望包含中文上下文与路径", got)
	}
}

func TestFingerprintWrapsReadError(t *testing.T) {
	dir := t.TempDir()

	_, err := Fingerprint(dir, "")
	if err == nil {
		t.Fatal("主配置是目录时应报错")
	}
	if got := err.Error(); !contains(got, "读取") || !contains(got, dir) {
		t.Fatalf("err = %v，期望包含中文上下文与路径", got)
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
