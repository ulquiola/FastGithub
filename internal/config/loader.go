package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// localConfigName 是当前目录下的主配置文件名。
const localConfigName = "fastgithub.toml"

// Discover 定位主配置与 conf.d 目录：显式路径优先，其次当前目录，最后用户配置目录。
func Discover(explicit string) (mainPath, confDir string, err error) {
	return discover(explicit, os.Stat)
}

// discover 实现 Discover；stat 可注入以便测试当前目录配置的探测分支。
func discover(explicit string, stat func(string) (fs.FileInfo, error)) (mainPath, confDir string, err error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", "", fmt.Errorf("解析配置路径 %s 失败: %w", explicit, err)
		}
		return abs, filepath.Join(filepath.Dir(abs), "conf.d"), nil
	}
	// 只有“不存在”才表示没有本地配置。权限不足、ACL、IO 错误等仍选本地路径，
	// 由 Load 报出明确的读取错误，而不是静默改用用户配置目录里的另一份配置。
	if _, statErr := stat(localConfigName); statErr == nil || !os.IsNotExist(statErr) {
		abs, err := filepath.Abs(localConfigName)
		if err != nil {
			return "", "", fmt.Errorf("解析配置路径 %s 失败: %w", localConfigName, err)
		}
		return abs, filepath.Join(filepath.Dir(abs), "conf.d"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", "", fmt.Errorf("无法定位配置目录: %w", err)
	}
	dir := filepath.Join(base, "fastgithub")
	return filepath.Join(dir, localConfigName), filepath.Join(dir, "conf.d"), nil
}

// Load 读取主配置与 conf.d 下的 TOML 片段（按文件名升序合并）。
func Load(mainPath, confDir string) (*Config, error) {
	cfg := Default()
	if err := loadFile(cfg, mainPath, true); err != nil {
		return nil, err
	}
	names, err := confFiles(confDir)
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		if err := loadFile(cfg, filepath.Join(confDir, name), false); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// confFiles 返回 conf.d 下按文件名升序的 .toml 文件；目录不存在视为空。
func confFiles(confDir string) ([]string, error) {
	entries, err := os.ReadDir(confDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取 %s 失败: %w", confDir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func loadFile(cfg *Config, path string, required bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if !required && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 %s 失败: %w", filepath.Base(path), err)
	}
	root, err := Parse(data)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if err := Decode(root, cfg); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

// Fingerprint 返回主配置与 conf.d 全部文件内容的摘要，用于热重载检测。
func Fingerprint(mainPath, confDir string) (string, error) {
	h := sha256.New()
	paths := []string{mainPath}
	names, err := confFiles(confDir)
	if err != nil {
		return "", err
	}
	for _, name := range names {
		paths = append(paths, filepath.Join(confDir, name))
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", fmt.Errorf("读取 %s 失败: %w", p, err)
		}
		h.Write([]byte(p))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
