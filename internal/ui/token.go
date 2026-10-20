package ui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// LoadOrCreateToken 读取 ui.token；不存在时生成 16 字节随机令牌并写入 0600。
func LoadOrCreateToken(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(data))
		if !validToken(token) {
			return "", fmt.Errorf("%s 不是有效的 32 位十六进制令牌", path)
		}
		if err := os.Chmod(path, 0600); err != nil {
			return "", fmt.Errorf("收紧 %s 权限失败: %w", path, err)
		}
		return token, nil
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("读取 %s 失败: %w", path, err)
	}

	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("生成 UI 令牌失败: %w", err)
	}
	token := hex.EncodeToString(raw[:])
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", fmt.Errorf("创建令牌目录失败: %w", err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0600); err != nil {
		return "", fmt.Errorf("写入 %s 失败: %w", path, err)
	}
	return token, nil
}

func validToken(token string) bool {
	if len(token) != 32 {
		return false
	}
	for i := 0; i < len(token); i++ {
		c := token[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
