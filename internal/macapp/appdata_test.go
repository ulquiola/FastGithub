package macapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDefaultsCopiesMissingOnly(t *testing.T) {
	home := t.TempDir()
	resources := filepath.Join(home, "Resources")
	dataDir := DataDir(home)

	if err := os.MkdirAll(filepath.Join(resources, "conf.d"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "fastgithub.toml"), []byte("default"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "conf.d", "10-github.toml"), []byte("github"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "conf.d", "20-google.toml"), []byte("google"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDefaults(dataDir, resources); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dataDir, "fastgithub.toml")); string(got) != "default" {
		t.Fatalf("config = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dataDir, "conf.d", "10-github.toml")); string(got) != "github" {
		t.Fatalf("conf = %q", got)
	}

	// 用户改过的文件不能被覆盖。
	if err := os.WriteFile(filepath.Join(dataDir, "fastgithub.toml"), []byte("user"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDefaults(dataDir, resources); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dataDir, "fastgithub.toml")); string(got) != "user" {
		t.Fatalf("用户配置被覆盖: %q", got)
	}

	// 新增的默认规则应补进来。
	if err := os.WriteFile(filepath.Join(resources, "conf.d", "30-microsoft.toml"), []byte("ms"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDefaults(dataDir, resources); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dataDir, "conf.d", "30-microsoft.toml")); string(got) != "ms" {
		t.Fatalf("缺失规则未补充: %q", got)
	}
}
