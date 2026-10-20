package certs

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
)

// writeFileAtomic 先写同目录临时文件再重命名，避免留下半截证书。
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("写入 %s 失败: %w", path, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("同步 %s 失败: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭 %s 失败: %w", path, err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("设置 %s 权限失败: %w", path, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("保存 %s 失败: %w", path, err)
	}
	return nil
}

func parsePrivateKey(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("不是 PEM 格式")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("不是 RSA 私钥")
	}
	return key, nil
}

func isPEMCertificate(data []byte) bool {
	block, _ := pem.Decode(data)
	return block != nil && block.Type == "CERTIFICATE"
}

func parseCertificate(data []byte) (*x509.Certificate, error) {
	if isPEMCertificate(data) {
		block, _ := pem.Decode(data)
		data = block.Bytes
	}
	return x509.ParseCertificate(data)
}

func publicKeysEqual(a, b any) bool {
	ka, ok := a.(*rsa.PublicKey)
	if !ok {
		return false
	}
	kb, ok := b.(*rsa.PublicKey)
	if !ok {
		return false
	}
	return ka.E == kb.E && ka.N.Cmp(kb.N) == 0
}
