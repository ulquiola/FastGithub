//go:build darwin

package app

import (
	"fmt"
	"os"
	"os/exec"
)

const launchdPlistPath = "/Library/LaunchDaemons/com.fastgithub.fastgithub.plist"

func InstallService(exePath string) error {
	if err := os.WriteFile(launchdPlistPath, []byte(LaunchdPlist(exePath)), 0644); err != nil {
		return fmt.Errorf("写入 %s 失败（需要 root）: %w", launchdPlistPath, err)
	}
	return runCommand("launchctl", "load", "-w", launchdPlistPath)
}

func UninstallService() error {
	_ = runCommand("launchctl", "unload", "-w", launchdPlistPath)
	if err := os.Remove(launchdPlistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 %s 失败: %w", launchdPlistPath, err)
	}
	return nil
}

func StartService() error {
	return runCommand("launchctl", "load", "-w", launchdPlistPath)
}

func StopService() error {
	return runCommand("launchctl", "unload", "-w", launchdPlistPath)
}

func runCommand(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v 失败: %w: %s", name, args, err, out)
	}
	return nil
}
