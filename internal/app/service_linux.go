//go:build linux

package app

import (
	"fmt"
	"os"
	"os/exec"
)

const systemdUnitPath = "/etc/systemd/system/fastgithub.service"

func InstallService(exePath string) error {
	if err := os.WriteFile(systemdUnitPath, []byte(SystemdUnit(exePath)), 0644); err != nil {
		return fmt.Errorf("写入 %s 失败（需要 root）: %w", systemdUnitPath, err)
	}
	if err := runCommand("systemctl", "daemon-reload"); err != nil {
		return err
	}
	return runCommand("systemctl", "enable", "--now", serviceName)
}

func UninstallService() error {
	_ = runCommand("systemctl", "disable", "--now", serviceName)
	if err := os.Remove(systemdUnitPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 %s 失败: %w", systemdUnitPath, err)
	}
	return runCommand("systemctl", "daemon-reload")
}

func StartService() error { return runCommand("systemctl", "start", serviceName) }

func StopService() error { return runCommand("systemctl", "stop", serviceName) }

func runCommand(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v 失败: %w: %s", name, args, err, out)
	}
	return nil
}
