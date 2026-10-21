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
	"fastgithub-go/internal/tray"
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

// stubDashboard 把面板入口换成记录调用的假实现，避免测试进程监听端口并阻塞。
func stubDashboard(t *testing.T, err error) *[]string {
	t.Helper()
	var calls []string
	old := startDashboard
	startDashboard = func(args []string, stdout, stderr io.Writer) error {
		calls = append(calls, strings.Join(args, " "))
		return err
	}
	t.Cleanup(func() { startDashboard = old })
	return &calls
}

func TestRunNoArgsStartsTray(t *testing.T) {
	calls := stubTray(t, nil)
	dash := stubDashboard(t, nil)
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d，期望 0；stderr=%s", code, stderr.String())
	}
	if len(*calls) != 1 || (*calls)[0] != "" {
		t.Fatalf("无参数应调用托盘一次，实际 %q", *calls)
	}
	if len(*dash) != 0 {
		t.Fatalf("托盘可用时不应启动面板，实际 %q", *dash)
	}
}

// 无参数时托盘不可用（本机无 Explorer 的典型情形）→ 自动进入控制台面板。
func TestRunNoArgsFallsBackToDashboard(t *testing.T) {
	stubTray(t, errors.New("添加通知区域图标失败: Access is denied"))
	dash := stubDashboard(t, nil)
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d，期望 0；stderr=%s", code, stderr.String())
	}
	if len(*dash) != 1 || (*dash)[0] != "" {
		t.Fatalf("托盘失败后应启动面板一次（无参数），实际 %q", *dash)
	}
	if !strings.Contains(stderr.String(), "Access is denied") {
		t.Fatalf("stderr 应说明托盘失败原因，实际 %q", stderr.String())
	}
}

// 托盘其实在运行（单实例互斥体命中）不算“托盘不可用”：友好退出，不再另起面板。
func TestRunNoArgsTrayAlreadyRunning(t *testing.T) {
	stubTray(t, tray.ErrAlreadyRunning)
	dash := stubDashboard(t, nil)
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d，期望 0；stderr=%s", code, stderr.String())
	}
	if len(*dash) != 0 {
		t.Fatalf("已有托盘实例时不应启动面板，实际 %q", *dash)
	}
	if !strings.Contains(stdout.String(), "已有一个 FastGithub 托盘实例") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

// 无参时 --no-open 需要一路传到面板（托盘可用时它不产生副作用）。
func TestRunNoOpenFlag(t *testing.T) {
	t.Run("托盘不可用时透传", func(t *testing.T) {
		stubTray(t, errors.New("没有桌面会话"))
		dash := stubDashboard(t, nil)
		var stdout, stderr bytes.Buffer
		if code := run([]string{"--no-open"}, &stdout, &stderr); code != 0 {
			t.Fatalf("exit = %d，期望 0；stderr=%s", code, stderr.String())
		}
		if len(*dash) != 1 || (*dash)[0] != "--no-open" {
			t.Fatalf("面板应收到 --no-open，实际 %q", *dash)
		}
	})
	t.Run("托盘可用时只托盘", func(t *testing.T) {
		calls := stubTray(t, nil)
		dash := stubDashboard(t, nil)
		var stdout, stderr bytes.Buffer
		if code := run([]string{"--no-open"}, &stdout, &stderr); code != 0 {
			t.Fatalf("exit = %d，期望 0；stderr=%s", code, stderr.String())
		}
		if len(*calls) != 1 || (*calls)[0] != "" {
			t.Fatalf("托盘参数 = %q", *calls)
		}
		if len(*dash) != 0 {
			t.Fatalf("托盘可用时不应启动面板，实际 %q", *dash)
		}
	})
}

// dashboard 子命令强制面板，不尝试托盘。
func TestRunDashboardSubcommand(t *testing.T) {
	trayCalls := stubTray(t, nil)
	dash := stubDashboard(t, nil)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"dashboard", "--no-open"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d，期望 0；stderr=%s", code, stderr.String())
	}
	if len(*trayCalls) != 0 {
		t.Fatalf("dashboard 子命令不应尝试托盘，实际 %q", *trayCalls)
	}
	if len(*dash) != 1 || (*dash)[0] != "--no-open" {
		t.Fatalf("面板参数 = %q", *dash)
	}
}

func TestRunDashboardFailure(t *testing.T) {
	stubDashboard(t, errors.New("监听代理端口失败"))
	var stdout, stderr bytes.Buffer
	if code := run([]string{"dashboard"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d，期望 1", code)
	}
	if !strings.Contains(stderr.String(), "监听代理端口失败") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunDashboardBadFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"dashboard", "--nope"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d，期望 1", code)
	}
	if !strings.Contains(stderr.String(), "参数错误") {
		t.Fatalf("stderr = %q", stderr.String())
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
	stubTray(t, errors.New("添加通知区域图标失败"))
	dash := stubDashboard(t, nil)
	var stdout, stderr bytes.Buffer
	if code := run([]string{"tray"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d，期望 1", code)
	}
	if !strings.Contains(stderr.String(), "添加通知区域图标失败") {
		t.Fatalf("stderr 应包含失败原因，实际 %q", stderr.String())
	}
	if len(*dash) != 0 {
		t.Fatalf("tray 子命令失败不应降级为面板，实际 %q", *dash)
	}
}

func TestRunHelp(t *testing.T) {
	for _, arg := range []string{"-h", "--help", "help"} {
		t.Run(arg, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run([]string{arg}, &stdout, &stderr); code != 0 {
				t.Fatalf("exit = %d，期望 0", code)
			}
			if !strings.Contains(stdout.String(), "用法") || !strings.Contains(stdout.String(), "tray") ||
				!strings.Contains(stdout.String(), "dashboard") {
				t.Fatalf("帮助应打印用法并提到 tray/dashboard，实际 %q", stdout.String())
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
		{"--no-open 多余参数", []string{"--no-open", "extra"}},
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
