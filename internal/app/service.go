package app

import (
	"encoding/xml"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

const serviceName = "fastgithub"
const launchdLabel = "com.fastgithub.fastgithub"

func configPathFor(exePath string) string {
	return filepath.Join(filepath.Dir(exePath), "fastgithub.toml")
}

// posixConfigPath 与 configPathFor 等价，但始终按 POSIX 规则拼接。
// systemd 单元与 launchd plist 的文本是 POSIX 语义，在 Windows 上生成/测试
// 同一份文本时不能被 filepath 改写成反斜杠。
func posixConfigPath(exePath string) string {
	return path.Join(path.Dir(exePath), "fastgithub.toml")
}

func quoted(s string) string { return `"` + s + `"` }

// serviceCommand 返回服务启动命令（可执行文件 + run + 配置路径）。
func serviceCommand(exePath string) string {
	return quoted(exePath) + " run --path " + quoted(configPathFor(exePath))
}

// posixServiceCommand 是 serviceCommand 的 POSIX 版本，供 systemd/launchd 文本使用。
func posixServiceCommand(exePath string) string {
	return quoted(exePath) + " run --path " + quoted(posixConfigPath(exePath))
}

// SystemdUnit 生成 Linux systemd 单元文本。
func SystemdUnit(exePath string) string {
	dir := path.Dir(exePath)
	return fmt.Sprintf(`[Unit]
Description=FastGithub
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=%s
ExecStart=%s
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
`, dir, posixServiceCommand(exePath))
}

// LaunchdPlist 生成 macOS launchd plist 文本。
func LaunchdPlist(exePath string) string {
	dir := path.Dir(exePath)
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>run</string>
    <string>--path</string>
    <string>%s</string>
  </array>
  <key>WorkingDirectory</key>
  <string>%s</string>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
</dict>
</plist>
`, xmlEscape(launchdLabel), xmlEscape(exePath), xmlEscape(posixConfigPath(exePath)), xmlEscape(dir))
}

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}
