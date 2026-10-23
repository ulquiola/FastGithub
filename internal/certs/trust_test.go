package certs

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type commandCall struct {
	name string
	args []string
}

func TestInstallCommands(t *testing.T) {
	cases := []struct {
		goos     string
		want     []commandCall
		wantCopy bool
	}{
		{
			goos: "windows",
			want: []commandCall{{"certutil", []string{"-addstore", "-f", "Root", "/tmp/ca.crt"}}},
		},
		{
			goos: "darwin",
			want: []commandCall{{"security", []string{"add-trusted-cert", "-d", "-r", "trustRoot", "-k", "/Library/Keychains/System.keychain", "/tmp/ca.crt"}}},
		},
		{
			goos:     "linux",
			want:     []commandCall{{"update-ca-certificates", nil}},
			wantCopy: true,
		},
	}
	for _, c := range cases {
		t.Run(c.goos, func(t *testing.T) {
			var calls []commandCall
			run := func(name string, args ...string) error {
				calls = append(calls, commandCall{name, args})
				return nil
			}
			var copied []string
			copyFile := func(src, dst string) error {
				copied = append(copied, src+" -> "+dst)
				return nil
			}
			if err := install("/tmp/ca.crt", c.goos, run, copyFile); err != nil {
				t.Fatalf("install: %v", err)
			}
			if !reflect.DeepEqual(calls, c.want) {
				t.Fatalf("命令 = %#v，期望 %#v", calls, c.want)
			}
			if c.wantCopy != (len(copied) == 1) {
				t.Fatalf("复制调用 = %v", copied)
			}
			if c.wantCopy && copied[0] != "/tmp/ca.crt -> /usr/local/share/ca-certificates/fastgithub.crt" {
				t.Fatalf("复制目标 = %q", copied[0])
			}
		})
	}
}

func TestInstallUnsupported(t *testing.T) {
	if err := install("/tmp/ca.crt", "plan9", func(string, ...string) error { return nil }, func(string, string) error { return nil }); err == nil {
		t.Fatal("不支持的平台应报错")
	}
}

func TestInstallPropagatesErrors(t *testing.T) {
	boom := errors.New("boom")
	if err := install("/tmp/ca.crt", "darwin", func(string, ...string) error { return boom }, func(string, string) error { return nil }); !errors.Is(err, boom) {
		t.Fatalf("err = %v，期望包装 boom", err)
	}
	if err := install("/tmp/ca.crt", "linux", func(string, ...string) error { return nil }, func(string, string) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v，期望包装 boom", err)
	}
}

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.crt")
	dst := filepath.Join(dir, "sub", "dst.crt")
	if err := os.WriteFile(src, []byte("cert"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "cert" {
		t.Fatalf("dst = %q", data)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(dst)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0644 {
			t.Fatalf("目标权限 = %v，期望 0644", info.Mode().Perm())
		}
	}
	if err := copyFile(filepath.Join(dir, "missing"), dst); err == nil {
		t.Fatal("源文件不存在应报错")
	}
}

func TestUninstallCommands(t *testing.T) {
	ca, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum(ca.Certificate().Raw)
	thumb := strings.ToUpper(hex.EncodeToString(sum[:]))
	home := t.TempDir()
	t.Setenv("HOME", home)

	// macOS：先删系统钥匙串，再删当前用户登录钥匙串。
	var darwinCalls []commandCall
	runDarwin := func(name string, args ...string) error {
		darwinCalls = append(darwinCalls, commandCall{name, args})
		return nil
	}
	if err := uninstall(ca.CertPath(), "darwin", runDarwin, func(string) error { return nil }); err != nil {
		t.Fatalf("darwin uninstall: %v", err)
	}
	wantDarwin := []commandCall{
		{"security", []string{"delete-certificate", "-Z", thumb, "-t", "/Library/Keychains/System.keychain"}},
		{"security", []string{"delete-certificate", "-Z", thumb, "-t", filepath.Join(home, "Library", "Keychains", "login.keychain-db")}},
	}
	if !reflect.DeepEqual(darwinCalls, wantDarwin) {
		t.Fatalf("darwin 命令 = %#v", darwinCalls)
	}

	// Linux：删除 ca-certificates 文件并刷新。
	var linuxCalls []commandCall
	runLinux := func(name string, args ...string) error {
		linuxCalls = append(linuxCalls, commandCall{name, args})
		return nil
	}
	var removed []string
	remove := func(path string) error {
		removed = append(removed, path)
		return nil
	}
	if err := uninstall("/tmp/ca.crt", "linux", runLinux, remove); err != nil {
		t.Fatalf("linux uninstall: %v", err)
	}
	if len(removed) != 1 || removed[0] != "/usr/local/share/ca-certificates/fastgithub.crt" {
		t.Fatalf("removed = %v", removed)
	}
	if len(linuxCalls) != 1 || linuxCalls[0].name != "update-ca-certificates" {
		t.Fatalf("linux 命令 = %#v", linuxCalls)
	}

	// Windows：按 SHA-1 指纹删除。
	var winCalls []commandCall
	runWin := func(name string, args ...string) error {
		winCalls = append(winCalls, commandCall{name, args})
		return nil
	}
	if err := uninstall(ca.CertPath(), "windows", runWin, func(string) error { return nil }); err != nil {
		t.Fatalf("windows uninstall: %v", err)
	}
	wantWin := []commandCall{{"certutil", []string{"-delstore", "Root", thumb}}}
	if !reflect.DeepEqual(winCalls, wantWin) {
		t.Fatalf("windows 命令 = %#v", winCalls)
	}

	if err := uninstall("/tmp/ca.crt", "plan9", runWin, remove); err == nil {
		t.Fatal("不支持的平台应报错")
	}
}

func TestUninstallPropagatesErrors(t *testing.T) {
	ca, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("boom")
	if err := uninstall(ca.CertPath(), "darwin", func(string, ...string) error { return boom }, func(string) error { return nil }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if err := uninstall(ca.CertPath(), "linux", func(string, ...string) error { return nil }, func(string) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestUninstallWindowsNotFoundIsIdempotent(t *testing.T) {
	ca, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = uninstall(ca.CertPath(), "windows", func(string, ...string) error {
		return errors.New("CertUtil: -delstore FAILED: 0x80092004 (CRYPT_E_NOT_FOUND)")
	}, func(string) error { return nil })
	if err != nil {
		t.Fatalf("证书不存在时应视为成功: %v", err)
	}
}

func TestDarwinInstallFallsBackToUserKeychain(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var calls []commandCall
	run := func(name string, args ...string) error {
		calls = append(calls, commandCall{name, args})
		if len(calls) == 1 {
			return errors.New("exit status 1: SecCertificateAddToKeychain: Write permissions error.")
		}
		return nil
	}
	if err := install("/tmp/ca.crt", "darwin", run, func(string, string) error { return nil }); err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("calls = %#v", calls)
	}
	want := []string{"add-trusted-cert", "-r", "trustRoot", "-k",
		filepath.Join(home, "Library", "Keychains", "login.keychain-db"), "/tmp/ca.crt"}
	if !reflect.DeepEqual(calls[1].args, want) {
		t.Fatalf("用户钥匙串命令 = %#v", calls[1].args)
	}
}

func TestDarwinUninstallSystemNeedsSudo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	ca, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	run := func(name string, args ...string) error {
		if args[len(args)-1] == "/Library/Keychains/System.keychain" {
			return errors.New("The authorization was denied.")
		}
		return nil
	}
	err = uninstall(ca.CertPath(), "darwin", run, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "sudo") {
		t.Fatalf("err = %v，期望提示需要 sudo", err)
	}
}

func TestStatus(t *testing.T) {
	ca, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	sum := sha1.Sum(ca.Certificate().Raw)
	thumb := strings.ToUpper(hex.EncodeToString(sum[:]))
	found := func(string, ...string) ([]byte, error) {
		return []byte("SHA-1 hash: " + thumb + "\n"), nil
	}
	missing := func(string, ...string) ([]byte, error) {
		return nil, errors.New("not found")
	}

	// macOS：系统钥匙串存在。
	st, err := status(ca.CertPath(), "darwin", found, nil, nil)
	if err != nil || !st.Installed || st.Scope != "system" {
		t.Fatalf("darwin system = %+v, %v", st, err)
	}
	// macOS：系统没有，用户钥匙串存在。
	calls := 0
	st, err = status(ca.CertPath(), "darwin", func(string, ...string) ([]byte, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("not found")
		}
		return []byte("SHA-1 hash: " + thumb + "\n"), nil
	}, nil, nil)
	if err != nil || !st.Installed || st.Scope != "user" {
		t.Fatalf("darwin user = %+v, %v", st, err)
	}
	// macOS：都没安装。
	st, err = status(ca.CertPath(), "darwin", missing, nil, nil)
	if err != nil || st.Installed || st.Scope != "none" {
		t.Fatalf("darwin none = %+v, %v", st, err)
	}

	// Windows：用 CryptoAPI 直查根存储（不再拉起 certutil 子进程），
	// 先本机再当前用户；传给探测函数的必须是原始 SHA-1 指纹。
	var gotHash []byte
	lookupFor := func(want TrustStatus) storeLookup {
		return func(hash []byte) (TrustStatus, error) {
			gotHash = append([]byte(nil), hash...)
			return want, nil
		}
	}
	st, err = status(ca.CertPath(), "windows", nil, nil, lookupFor(TrustStatus{Installed: true, Scope: "system"}))
	if err != nil || !st.Installed || st.Scope != "system" {
		t.Fatalf("windows system = %+v, %v", st, err)
	}
	if !bytes.Equal(gotHash, sum[:]) {
		t.Fatalf("传给存储探测的指纹 = %x，期望 SHA-1 %x", gotHash, sum[:])
	}
	st, err = status(ca.CertPath(), "windows", nil, nil, lookupFor(TrustStatus{Installed: true, Scope: "user"}))
	if err != nil || !st.Installed || st.Scope != "user" {
		t.Fatalf("windows user = %+v, %v", st, err)
	}
	st, err = status(ca.CertPath(), "windows", nil, nil, lookupFor(TrustStatus{Scope: "none"}))
	if err != nil || st.Installed || st.Scope != "none" {
		t.Fatalf("windows none = %+v, %v", st, err)
	}
	boom := errors.New("crypt32 boom")
	if _, err := status(ca.CertPath(), "windows", nil, nil, func([]byte) (TrustStatus, error) {
		return TrustStatus{}, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("windows 存储错误应向上传播，err = %v", err)
	}

	// Linux：文件内容一致才算安装。
	same := func(path string) ([]byte, error) { return []byte("cert"), nil }
	st, err = status("/tmp/ca.crt", "linux", nil, same, nil)
	if err != nil || !st.Installed || st.Scope != "system" {
		t.Fatalf("linux = %+v, %v", st, err)
	}
	diff := func(path string) ([]byte, error) {
		if path == "/usr/local/share/ca-certificates/fastgithub.crt" {
			return []byte("other"), nil
		}
		return []byte("cert"), nil
	}
	st, err = status("/tmp/ca.crt", "linux", nil, diff, nil)
	if err != nil || st.Installed {
		t.Fatalf("linux mismatch = %+v, %v", st, err)
	}

	if _, err := status("/tmp/ca.crt", "plan9", nil, nil, nil); err == nil {
		t.Fatal("不支持的平台应报错")
	}
}

func TestInstallWindowsAccessDenied(t *testing.T) {
	err := install("ca.crt", "windows", func(string, ...string) error {
		return errors.New("exit status 0x80070005: CertUtil: -addstore 失败")
	}, func(string, string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "管理员") {
		t.Fatalf("err = %v，期望提示需要管理员权限", err)
	}
	if strings.Contains(err.Error(), "0x80070005") {
		t.Fatalf("不应把原始 certutil 输出暴露给用户: %v", err)
	}
}
