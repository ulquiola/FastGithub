package app

import (
	"runtime"
	"strings"
	"testing"
)

func TestBuildInfo(t *testing.T) {
	if Version() == "" {
		t.Fatal("Version() 不能为空")
	}
	info := BuildInfo()
	if !strings.HasPrefix(info, "fastgithub ") {
		t.Fatalf("BuildInfo() = %q，应以 fastgithub 开头", info)
	}
	if !strings.Contains(info, runtime.Version()) {
		t.Fatalf("BuildInfo() = %q，应包含运行时版本 %q", info, runtime.Version())
	}
	if !strings.Contains(info, "(") || !strings.Contains(info, ")") {
		t.Fatalf("BuildInfo() = %q，应包含括号结构", info)
	}
}
