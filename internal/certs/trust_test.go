package certs

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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
