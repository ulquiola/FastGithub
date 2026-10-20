package certs

import (
	"errors"
	"reflect"
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
