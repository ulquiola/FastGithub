package macapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchAgentLifecycle(t *testing.T) {
	home := t.TempDir()
	exe := filepath.Join(home, "Applications", "FastGithub.app", "Contents", "MacOS", "FastGithub")
	if LaunchAgentEnabled(home) {
		t.Fatal("初始不应启用")
	}
	if err := WriteLaunchAgent(home, exe); err != nil {
		t.Fatal(err)
	}
	if !LaunchAgentEnabled(home) {
		t.Fatal("写入后应启用")
	}
	data, err := os.ReadFile(LaunchAgentPath(home))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"com.fastgithub.menubar",
		"<key>RunAtLoad</key><true/>",
		"<key>LimitLoadToSessionType</key><string>Aqua</string>",
		exe,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plist 缺少 %q:\n%s", want, text)
		}
	}
	if err := RemoveLaunchAgent(home); err != nil {
		t.Fatal(err)
	}
	if LaunchAgentEnabled(home) {
		t.Fatal("删除后应禁用")
	}
	if err := RemoveLaunchAgent(home); err != nil {
		t.Fatalf("重复删除应幂等: %v", err)
	}
}

func TestLaunchAgentCommandArgs(t *testing.T) {
	home := t.TempDir()
	if got := BootstrapArgs(home, 501); len(got) != 3 ||
		got[0] != "bootstrap" || got[1] != "gui/501" || got[2] != LaunchAgentPath(home) {
		t.Fatalf("bootstrap args = %v", got)
	}
	if got := BootoutArgs(501); len(got) != 2 || got[0] != "bootout" || got[1] != "gui/501/com.fastgithub.menubar" {
		t.Fatalf("bootout args = %v", got)
	}
}
