package macapp

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const launchAgentLabel = "com.fastgithub.menubar"

// LaunchAgentPath 返回用户级 LaunchAgent 的 plist 路径。
func LaunchAgentPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

// LaunchAgentEnabled 判断开机启动 plist 是否存在。
func LaunchAgentEnabled(home string) bool {
	_, err := os.Stat(LaunchAgentPath(home))
	return err == nil
}

// WriteLaunchAgent 写入开机启动 plist。
func WriteLaunchAgent(home, executable string) error {
	path := LaunchAgentPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("创建 LaunchAgents 目录失败: %w", err)
	}
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	fmt.Fprintf(&b, "  <key>Label</key><string>%s</string>\n", xmlEscape(launchAgentLabel))
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	fmt.Fprintf(&b, "    <string>%s</string>\n", xmlEscape(executable))
	b.WriteString("  </array>\n")
	b.WriteString("  <key>RunAtLoad</key><true/>\n")
	b.WriteString("  <key>LimitLoadToSessionType</key><string>Aqua</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return os.WriteFile(path, []byte(b.String()), 0644)
}

// RemoveLaunchAgent 删除开机启动 plist；不存在时视为成功。
func RemoveLaunchAgent(home string) error {
	err := os.Remove(LaunchAgentPath(home))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 LaunchAgent 失败: %w", err)
	}
	return nil
}

// BootstrapArgs 返回 launchctl bootstrap 的参数。
func BootstrapArgs(home string, uid int) []string {
	return []string{"bootstrap", fmt.Sprintf("gui/%d", uid), LaunchAgentPath(home)}
}

// BootoutArgs 返回 launchctl bootout 的参数。
func BootoutArgs(uid int) []string {
	return []string{"bootout", fmt.Sprintf("gui/%d/%s", uid, launchAgentLabel)}
}

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}
