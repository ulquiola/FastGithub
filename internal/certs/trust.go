package certs

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type commandRunner func(name string, args ...string) error
type fileCopier func(src, dst string) error

// Install 把 certPath 指向的 CA 证书加入系统信任库，需要管理员/root 权限。
func Install(certPath string) error {
	return install(certPath, runtime.GOOS, runCommand, copyFile)
}

func install(certPath, goos string, run commandRunner, copyFile fileCopier) error {
	switch goos {
	case "windows":
		if err := run("certutil", "-addstore", "-f", "Root", certPath); err != nil {
			return fmt.Errorf("certutil 导入失败（可能需要管理员权限）: %w", err)
		}
	case "darwin":
		if err := run("security", "add-trusted-cert", "-d", "-r", "trustRoot",
			"-k", "/Library/Keychains/System.keychain", certPath); err != nil {
			return fmt.Errorf("security 导入失败（可能需要 sudo）: %w", err)
		}
	case "linux":
		const dest = "/usr/local/share/ca-certificates/fastgithub.crt"
		if err := copyFile(certPath, dest); err != nil {
			return fmt.Errorf("复制证书到 %s 失败（需要 root）: %w", dest, err)
		}
		if err := run("update-ca-certificates"); err != nil {
			return fmt.Errorf("update-ca-certificates 失败（可能需要 root）: %w", err)
		}
	default:
		return fmt.Errorf("不支持的平台 %s，请手动导入证书 %s", goos, certPath)
	}
	return nil
}

func runCommand(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	if err := out.Chmod(0644); err != nil {
		out.Close()
		os.Remove(dst)
		return err
	}
	return out.Close()
}
