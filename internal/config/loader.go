package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Discover 定位主配置与 conf.d 目录：显式路径优先，其次当前目录，最后用户配置目录。
func Discover(explicit string) (mainPath, confDir string, err error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", "", err
		}
		return abs, filepath.Join(filepath.Dir(abs), "conf.d"), nil
	}
	if _, statErr := os.Stat("fastgithub.toml"); statErr == nil {
		abs, err := filepath.Abs("fastgithub.toml")
		if err != nil {
			return "", "", err
		}
		return abs, filepath.Join(filepath.Dir(abs), "conf.d"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", "", fmt.Errorf("无法定位配置目录: %w", err)
	}
	dir := filepath.Join(base, "fastgithub")
	return filepath.Join(dir, "fastgithub.toml"), filepath.Join(dir, "conf.d"), nil
}

// Load 读取主配置与 conf.d 下的 TOML 片段（按文件名升序合并）。
func Load(mainPath, confDir string) (*Config, error) {
	cfg := Default()
	if err := loadFile(cfg, mainPath, true); err != nil {
		return nil, err
	}
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
	for _, name := range names {
		if err := loadFile(cfg, filepath.Join(confDir, name), false); err != nil {
			return nil, err
		}
	}
	return cfg, nil
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
	entries, err := os.ReadDir(confDir)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
			paths = append(paths, filepath.Join(confDir, e.Name()))
		}
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		h.Write([]byte(p))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
