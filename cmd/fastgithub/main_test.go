package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fastgithub-go/internal/certs"
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

// stubTray 把托盘入口换成记录调用的假实现，避免测试进程创建真实通知区域图标。
func stubTray(t *testing.T, err error) *[]string {
	t.Helper()
	var calls []string
	old := startTray
	startTray = func(args []string, stdout, stderr io.Writer) error {
		calls = append(calls, strings.Join(args, " "))
		return err
	}
	t.Cleanup(func() { startTray = old })
	return &calls
}

func TestRunNoArgsStartsTray(t *testing.T) {
	calls := stubTray(t, nil)
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d，期望 0；stderr=%s", code, stderr.String())
	}
	if len(*calls) != 1 || (*calls)[0] != "" {
		t.Fatalf("无参数应调用托盘一次，实际 %q", *calls)
	}
}

func TestRunTraySubcommand(t *testing.T) {
	calls := stubTray(t, nil)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"tray", "--path", "fastgithub.toml"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d，期望 0；stderr=%s", code, stderr.String())
	}
	if len(*calls) != 1 || !strings.Contains((*calls)[0], "--path fastgithub.toml") {
		t.Fatalf("tray 子命令参数未透传：%q", *calls)
	}
}

func TestRunTrayFailure(t *testing.T) {
	stubTray(t, errors.New("没有桌面会话"))
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d，期望 1", code)
	}
	if !strings.Contains(stderr.String(), "没有桌面会话") {
		t.Fatalf("stderr 应包含失败原因，实际 %q", stderr.String())
	}
}

func TestRunHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help", "help"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{arg}, &stdout, &stderr); code != 0 {
				t.Fatalf("exit = %d，期望 0", code)
			}
			if !strings.Contains(stdout.String(), "用法") || !strings.Contains(stdout.String(), "tray") {
				t.Fatalf("帮助应打印用法并提到 tray，实际 %q", stdout.String())
			}
			if stderr.String() != "" {
				t.Fatalf("帮助不应写 stderr，实际 %q", stderr.String())
			}
		})
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
		{"未知选项", []string{"-x"}},
		{"version 多余参数", []string{"version", "extra"}},
		{"help 多余参数", []string{"help", "extra"}},
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
	if !strings.Contains(stdout.String(), "已加入信任库") {
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

func TestRunRunInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fastgithub.toml")
	if err := os.WriteFile(path, []byte("[server]\nproxy_port = 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"run", "--path", path}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "proxy_port") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestRunRunUsage(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"run", "extra"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "用法") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestRunSelfTest(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"windivert-selftest"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "WinDivert") || !strings.Contains(stdout.String(), "github.com A -> 127.0.0.1") {
		t.Fatalf("stdout = %s", stdout.String())
	}
}

func TestServiceCommandsRejectExtraArgs(t *testing.T) {
	for _, cmd := range []string{"install", "uninstall", "start", "stop"} {
		var stdout, stderr bytes.Buffer
		if code := run([]string{cmd, "extra"}, &stdout, &stderr); code != 1 || !strings.Contains(stderr.String(), "用法") {
			t.Fatalf("%s exit=%d stderr=%q", cmd, code, stderr.String())
		}
	}
}

func TestRunUninstallCert(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cacert")
	if _, err := certs.LoadOrCreate(dir); err != nil {
		t.Fatal(err)
	}
	var removed string
	old := uninstallCert
	uninstallCert = func(path string) error {
		removed = path
		return nil
	}
	defer func() { uninstallCert = old }()

	var stdout, stderr bytes.Buffer
	code := run([]string{"uninstall-cert", "--dir", dir}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d stderr = %s", code, stderr.String())
	}
	if removed == "" || !strings.Contains(removed, "fastgithub.crt") {
		t.Fatalf("removed = %q", removed)
	}
	if !strings.Contains(stdout.String(), "已从信任库移除") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunUninstallCertMissingCA(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"uninstall-cert", "--dir", filepath.Join(t.TempDir(), "missing")}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "不存在") {
		t.Fatalf("exit = %d stderr = %q", code, stderr.String())
	}
}

// --dir 传部署目录时，CA 必须落在其下 cacert/，与 run（chdir 到配置目录后使用
// 默认 cacert）保持一致；否则会生成第二张 CA，浏览器信任的与实际签发的不一致。
func TestInstallCertDirIsDeployDir(t *testing.T) {
	deploy := t.TempDir()
	var installed string
	old := installCert
	installCert = func(path string) error {
		installed = path
		return nil
	}
	defer func() { installCert = old }()

	var stdout, stderr bytes.Buffer
	if code := run([]string{"install-cert", "--dir", deploy}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	want := filepath.Join(deploy, "cacert", "fastgithub.crt")
	if installed != want {
		t.Fatalf("installed = %q，期望 %q", installed, want)
	}
}
