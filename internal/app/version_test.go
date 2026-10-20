package app

import (
	"strings"
	"testing"
)

func TestBuildInfo(t *testing.T) {
	if Version() == "" {
		t.Fatal("Version() 不能为空")
	}
	if !strings.HasPrefix(BuildInfo(), "fastgithub ") {
		t.Fatalf("BuildInfo() = %q，应以 fastgithub 开头", BuildInfo())
	}
}
