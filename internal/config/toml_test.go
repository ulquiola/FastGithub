package config

import (
	"strings"
	"testing"
)

func TestParseScalars(t *testing.T) {
	root, err := Parse([]byte(`
# 注释
port = 38457
name = "fast\"github"  # 行尾注释
enabled = true
disabled = false
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := root.Table["port"].Int; got != 38457 {
		t.Fatalf("port = %d", got)
	}
	if got := root.Table["name"].Str; got != `fast"github` {
		t.Fatalf("name = %q", got)
	}
	if !root.Table["enabled"].Bool {
		t.Fatal("enabled 应为 true")
	}
	if root.Table["disabled"].Bool {
		t.Fatal("disabled 应为 false")
	}
	if got := root.Table["port"].Line; got != 3 {
		t.Fatalf("port 行号 = %d，期望 3", got)
	}
}

func TestParseRejectsBadValue(t *testing.T) {
	_, err := Parse([]byte("port = not-a-number\n"))
	if err == nil || !strings.Contains(err.Error(), "第 1 行") {
		t.Fatalf("err = %v，期望带行号的错误", err)
	}
}
