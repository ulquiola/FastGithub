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

type namedString string

type namedSlice struct {
	Names []namedString `toml:"names"`
}

func TestDecodeNilTree(t *testing.T) {
	var s sample
	if err := Decode(nil, &s); err == nil {
		t.Fatal("Decode(nil, …) 应返回错误")
	}
}

func TestDecodeNamedStringSlice(t *testing.T) {
	root, err := Parse([]byte(`names = ["a", "b"]` + "\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var s namedSlice
	if err := Decode(root, &s); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if len(s.Names) != 2 || s.Names[1] != namedString("b") {
		t.Fatalf("names = %#v", s.Names)
	}
}

func TestDecodeUnknownKeysSortedWithLines(t *testing.T) {
	root, err := Parse([]byte("z = 1\na = 2\nm = 3\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var s struct{}
	err = Decode(root, &s)
	want := "未知配置项: z (第 1 行), a (第 2 行), m (第 3 行)"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v，期望 %q", err, want)
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
