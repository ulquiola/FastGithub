package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d，期望 0；stderr=%s", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "fastgithub ") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"nope"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d，期望 1", code)
	}
	if !strings.Contains(stderr.String(), "用法") {
		t.Fatalf("stderr 应包含用法说明，实际 %q", stderr.String())
	}
}

func TestRunUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"无参数", nil},
		{"version 多余参数", []string{"version", "extra"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tc.args, &stdout, &stderr); code != 1 {
				t.Fatalf("exit = %d，期望 1", code)
			}
			if !strings.Contains(stderr.String(), "用法") {
				t.Fatalf("stderr 应包含用法说明，实际 %q", stderr.String())
			}
			if stdout.String() != "" {
				t.Fatalf("用法错误不应写 stdout，实际 %q", stdout.String())
			}
		})
	}
}

func TestConfigValidateValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fastgithub.toml")
	if err := os.WriteFile(path, []byte("[server]\nproxy_port = 38457\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"config", "validate", "--path", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d，stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "配置有效") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestConfigValidateInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fastgithub.toml")
	if err := os.WriteFile(path, []byte("[server]\nproxy_port = 0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"config", "validate", "--path", path}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit = %d，期望 1", code)
	}
	if !strings.Contains(stderr.String(), "proxy_port") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunInstallCert(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cacert")
	var installed string
	old := installCert
	installCert = func(path string) error {
		installed = path
		return nil
	}
	defer func() { installCert = old }()

	var stdout, stderr bytes.Buffer
	code := run([]string{"install-cert", "--dir", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, stderr.String())
	}
	if installed == "" || !strings.Contains(installed, "fastgithub.crt") {
		t.Fatalf("installed = %q", installed)
	}
	if !strings.Contains(stdout.String(), "已加入系统信任库") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunInstallCertFailure(t *testing.T) {
	old := installCert
	installCert = func(string) error { return errors.New("denied") }
	defer func() { installCert = old }()

	var stdout, stderr bytes.Buffer
	code := run([]string{"install-cert", "--dir", filepath.Join(t.TempDir(), "cacert")}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "denied") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
}
