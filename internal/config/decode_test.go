package config

import (
	"strings"
	"testing"
)

type sampleSub struct {
	On bool `toml:"on"`
}

type sample struct {
	Port  int        `toml:"port"`
	Name  string     `toml:"name"`
	Flags []string   `toml:"flags"`
	Sub   *sampleSub `toml:"sub"`
	Inner struct {
		Deep int `toml:"deep"`
	} `toml:"inner"`
}

func TestDecode(t *testing.T) {
	root, err := Parse([]byte(`
port = 8080
name = "x"
flags = ["a", "b"]

[sub]
on = true

[inner]
deep = 7
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var s sample
	if err := Decode(root, &s); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if s.Port != 8080 || s.Name != "x" || len(s.Flags) != 2 || s.Flags[1] != "b" {
		t.Fatalf("s = %+v", s)
	}
	if s.Flags == nil {
		t.Fatal("flags 不应为 nil")
	}
	if s.Sub == nil || !s.Sub.On {
		t.Fatalf("sub = %+v", s.Sub)
	}
	if s.Inner.Deep != 7 {
		t.Fatalf("inner.deep = %d", s.Inner.Deep)
	}
}

func TestDecodeEmptyArray(t *testing.T) {
	root, err := Parse([]byte("flags = []\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var s sample
	if err := Decode(root, &s); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if s.Flags == nil {
		t.Fatal("空数组应解码为非 nil 切片")
	}
	if len(s.Flags) != 0 {
		t.Fatalf("len(flags) = %d，期望 0", len(s.Flags))
	}
}

func TestDecodeUnknownKey(t *testing.T) {
	root, err := Parse([]byte("port = 1\nunknown = 2\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var s sample
	err = Decode(root, &s)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("err = %v，期望包含未知键 unknown", err)
	}
}

func TestDecodeTypeMismatch(t *testing.T) {
	root, err := Parse([]byte(`port = "x"` + "\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var s sample
	if err := Decode(root, &s); err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("err = %v，期望包含 port", err)
	}
}
