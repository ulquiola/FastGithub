package app

import (
	"fmt"
	"path/filepath"
)

const serviceName = "fastgithub"
const launchdLabel = "com.fastgithub.fastgithub"

func configPathFor(exePath string) string {
	return filepath.Join(filepath.Dir(exePath), "fastgithub.toml")
}

func quoted(s string) string { return `"` + s + `"` }

// serviceCommand 返回服务启动命令（可执行文件 + run + 配置路径）。
func serviceCommand(exePath string) string {
	return quoted(exePath) + " run --path " + quoted(configPathFor(exePath))
}

// SystemdUnit 生成 Linux systemd 单元文本。
func SystemdUnit(exePath string) string {
	dir := filepath.Dir(exePath)
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
`, dir, serviceCommand(exePath))
}

// LaunchdPlist 生成 macOS launchd plist 文本。
func LaunchdPlist(exePath string) string {
	dir := filepath.Dir(exePath)
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
`, launchdLabel, exePath, configPathFor(exePath), dir)
}
