// Package macapp 提供 macOS 应用的数据目录、默认配置和 LaunchAgent 管理。
package macapp

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DataDir 返回应用数据目录。
func DataDir(home string) string {
	return filepath.Join(home, "Library", "Application Support", "FastGithub")
}

// EnsureDefaults 在数据目录缺失时复制默认配置与规则，不覆盖已有文件。
func EnsureDefaults(dataDir, resourcesDir string) error {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("创建数据目录 %s 失败: %w", dataDir, err)
	}
	if err := copyIfMissing(filepath.Join(resourcesDir, "fastgithub.toml"), filepath.Join(dataDir, "fastgithub.toml")); err != nil {
		return err
	}
	confSrc := filepath.Join(resourcesDir, "conf.d")
	entries, err := os.ReadDir(confSrc)
	if err != nil {
		return fmt.Errorf("读取默认规则目录 %s 失败: %w", confSrc, err)
	}
	confDst := filepath.Join(dataDir, "conf.d")
	if err := os.MkdirAll(confDst, 0700); err != nil {
		return fmt.Errorf("创建规则目录 %s 失败: %w", confDst, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := copyIfMissing(filepath.Join(confSrc, entry.Name()), filepath.Join(confDst, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyIfMissing(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开默认文件 %s 失败: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("创建 %s 失败: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("复制 %s 失败: %w", src, err)
	}
	return out.Close()
}
