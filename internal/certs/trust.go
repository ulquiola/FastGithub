package certs

import (
	"crypto/sha1"
	"encoding/hex"
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
type fileRemover func(path string) error

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
		args := []string{"add-trusted-cert", "-d", "-r", "trustRoot",
			"-k", "/Library/Keychains/System.keychain", certPath}
		if err := run("security", args...); err != nil {
			if !isAuthError(err) {
				return fmt.Errorf("security 导入失败: %w", err)
			}
			// 普通用户进程无法直接写系统钥匙串，弹系统授权对话框重试。
			if err := run("osascript", "-e", appleScriptFor("/usr/bin/security", args...)); err != nil {
				return fmt.Errorf("security 导入失败（授权被取消或不可用）: %w", err)
			}
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

// Uninstall 把 CA 证书从系统信任库移除，需要管理员/root 权限。
func Uninstall(certPath string) error {
	return uninstall(certPath, runtime.GOOS, runCommand, os.Remove)
}

func uninstall(certPath, goos string, run commandRunner, remove fileRemover) error {
	switch goos {
	case "windows":
		thumbprint, err := certThumbprint(certPath)
		if err != nil {
			return err
		}
		if err := run("certutil", "-delstore", "Root", thumbprint); err != nil {
			if isNotFound(err) {
				return nil
			}
			return fmt.Errorf("certutil 删除失败（可能需要管理员权限）: %w", err)
		}
	case "darwin":
		thumbprint, err := certThumbprint(certPath)
		if err != nil {
			return err
		}
		args := []string{"delete-certificate", "-Z", thumbprint, "-t",
			"/Library/Keychains/System.keychain"}
		if err := run("security", args...); err != nil {
			if isNotFound(err) {
				return nil
			}
			if !isAuthError(err) {
				return fmt.Errorf("security 删除失败: %w", err)
			}
			if err := run("osascript", "-e", appleScriptFor("/usr/bin/security", args...)); err != nil {
				return fmt.Errorf("security 删除失败（授权被取消或不可用）: %w", err)
			}
		}
	case "linux":
		const dest = "/usr/local/share/ca-certificates/fastgithub.crt"
		if err := remove(dest); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除 %s 失败（需要 root）: %w", dest, err)
		}
		if err := run("update-ca-certificates"); err != nil {
			return fmt.Errorf("update-ca-certificates 失败（可能需要 root）: %w", err)
		}
	default:
		return fmt.Errorf("不支持的平台 %s，请手动移除证书 %s", goos, certPath)
	}
	return nil
}

func isAuthError(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"authorization", "denied", "not permitted", "operation not permitted",
		"must be run as root", "sudo", "permission", "not authorized",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// appleScriptFor 生成带管理员权限的 do shell script，命令参数做 shell 与 AppleScript 双重转义。
func appleScriptFor(name string, args ...string) string {
	parts := []string{name}
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}
	command := strings.Join(parts, " ")
	command = strings.ReplaceAll(command, `\`, `\\`)
	command = strings.ReplaceAll(command, `"`, `\"`)
	return `do shell script "` + command + `" with administrator privileges`
}

func shellQuote(s string) string {
	return `'` + strings.ReplaceAll(s, `'`, `'\''`) + `'`
}

func isNotFound(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "could not be found") ||
		strings.Contains(msg, "crypt_e_not_found") ||
		strings.Contains(msg, "0x80092004") ||
		strings.Contains(msg, "cannot find")
}

// certThumbprint 返回 Windows certutil 使用的 SHA-1 指纹（大写十六进制）。
func certThumbprint(certPath string) (string, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return "", fmt.Errorf("读取证书 %s 失败: %w", certPath, err)
	}
	cert, err := parseCertificate(data)
	if err != nil {
		return "", fmt.Errorf("解析证书 %s 失败: %w", certPath, err)
	}
	sum := sha1.Sum(cert.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:])), nil
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
