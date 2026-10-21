package certs

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type commandRunner func(name string, args ...string) error
type commandOutput func(name string, args ...string) ([]byte, error)
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
			// certutil 在无管理员权限时的输出是 GBK，直接抛给用户会变成乱码，
			// 这里识别访问被拒并给出可执行的中文提示。
			if isAccessDeniedOutput(err) {
				return fmt.Errorf("需要管理员权限：请以管理员身份重新运行 install-cert")
			}
			return fmt.Errorf("certutil 导入失败: %w", err)
		}
	case "darwin":
		systemArgs := []string{"add-trusted-cert", "-d", "-r", "trustRoot",
			"-k", "/Library/Keychains/System.keychain", certPath}
		if err := run("security", systemArgs...); err != nil {
			if !isAuthError(err) {
				return fmt.Errorf("security 导入失败: %w", err)
			}
			// 非 root 进程回退到当前用户的登录钥匙串，无需 sudo。
			userKeychain, keychainErr := loginKeychainPath()
			if keychainErr != nil {
				return fmt.Errorf("security 导入失败（系统钥匙串需要 sudo）: %w", err)
			}
			userArgs := []string{"add-trusted-cert", "-r", "trustRoot", "-k", userKeychain, certPath}
			if userErr := run("security", userArgs...); userErr != nil {
				return fmt.Errorf("security 导入失败（系统钥匙串需要 sudo，用户钥匙串也失败）: %w", userErr)
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

// TrustStatus 描述 CA 的信任安装状态。
type TrustStatus struct {
	Installed bool
	Scope     string // system | user | none
}

// Status 检测 CA 是否已安装到系统或当前用户信任库。
func Status(certPath string) (TrustStatus, error) {
	return status(certPath, runtime.GOOS, runOutput, os.ReadFile)
}

func status(certPath, goos string, runOut commandOutput, readFile func(string) ([]byte, error)) (TrustStatus, error) {
	switch goos {
	case "darwin":
		thumbprint, err := certThumbprint(certPath)
		if err != nil {
			return TrustStatus{}, err
		}
		// security find-certificate 的 -Z 是“显示哈希”而非过滤，列出 CN 后匹配 SHA-1。
		find := func(keychain string) bool {
			out, _ := runOut("security", "find-certificate", "-a", "-c", "FastGithub", "-Z", keychain)
			return strings.Contains(strings.ToUpper(string(out)), thumbprint)
		}
		if find("/Library/Keychains/System.keychain") {
			return TrustStatus{Installed: true, Scope: "system"}, nil
		}
		if userKeychain, err := loginKeychainPath(); err == nil && find(userKeychain) {
			return TrustStatus{Installed: true, Scope: "user"}, nil
		}
		return TrustStatus{Scope: "none"}, nil
	case "windows":
		thumbprint, err := certThumbprint(certPath)
		if err != nil {
			return TrustStatus{}, err
		}
		if _, err := runOut("certutil", "-store", "Root", thumbprint); err == nil {
			return TrustStatus{Installed: true, Scope: "system"}, nil
		}
		return TrustStatus{Scope: "none"}, nil
	case "linux":
		const dest = "/usr/local/share/ca-certificates/fastgithub.crt"
		installed, err := readFile(dest)
		if err != nil {
			return TrustStatus{Scope: "none"}, nil
		}
		source, err := readFile(certPath)
		if err != nil {
			return TrustStatus{}, err
		}
		if bytes.Equal(installed, source) {
			return TrustStatus{Installed: true, Scope: "system"}, nil
		}
		return TrustStatus{Scope: "none"}, nil
	default:
		return TrustStatus{}, fmt.Errorf("不支持的平台 %s", goos)
	}
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
		var errs []error
		systemArgs := []string{"delete-certificate", "-Z", thumbprint, "-t",
			"/Library/Keychains/System.keychain"}
		if err := run("security", systemArgs...); err != nil && !isNotFound(err) {
			if isAuthError(err) {
				errs = append(errs, fmt.Errorf("系统钥匙串中的证书需要 sudo 移除: %w", err))
			} else {
				errs = append(errs, fmt.Errorf("删除系统钥匙串证书失败: %w", err))
			}
		}
		if userKeychain, keychainErr := loginKeychainPath(); keychainErr == nil {
			userArgs := []string{"delete-certificate", "-Z", thumbprint, "-t", userKeychain}
			if err := run("security", userArgs...); err != nil && !isNotFound(err) {
				errs = append(errs, fmt.Errorf("删除用户钥匙串证书失败: %w", err))
			}
		}
		if len(errs) > 0 {
			return errors.Join(errs...)
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

// isAccessDeniedOutput 识别 certutil 的访问被拒错误（0x80070005 / Access is denied），
// 其输出为 GBK 且对用户无意义，调用方应改为给出中文提示。
func isAccessDeniedOutput(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "0x80070005") || strings.Contains(msg, "access is denied")
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

func loginKeychainPath() (string, error) {
	// macOS 上 HOME 一定存在，优先取它：既符合 darwin 的真实语义，也让
	// 在其它平台模拟 darwin 的测试可以通过 t.Setenv("HOME", …) 注入路径。
	home := os.Getenv("HOME")
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return "", err
		}
	}
	return filepath.Join(home, "Library", "Keychains", "login.keychain-db"), nil
}

func isNotFound(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "not found") ||
		strings.Contains(msg, "could not be found") ||
		strings.Contains(msg, "crypt_e_not_found") ||
		strings.Contains(msg, "0x80092004") ||
		strings.Contains(msg, "cannot find") ||
		strings.Contains(msg, "unable to delete certificate matching")
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
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("%w: %s", err, msg)
		}
		return err
	}
	return nil
}

// runOutput 用于状态检测：钥匙串锁定时最多等待 5s，避免 UI 请求挂住。
func runOutput(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
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
