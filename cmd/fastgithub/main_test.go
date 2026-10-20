package main

import (
	"bytes"
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
