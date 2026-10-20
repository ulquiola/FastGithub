package certs

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const (
	// DefaultDir 是 CA 证书的默认目录。
	DefaultDir         = "cacert"
	certFileName       = "fastgithub.crt"
	keyFileName        = "fastgithub.key"
	legacyCertFileName = "fastgithub.cer"
)

// Authority 持有本地 CA 与按域名签发的叶子证书缓存。
type Authority struct {
	dir      string
	certPath string
	keyPath  string
	cert     *x509.Certificate
	key      *rsa.PrivateKey
	now      func() time.Time

	mu    sync.Mutex
	leafs map[string]*tls.Certificate
}

// LoadOrCreate 从 dir 加载 CA；证书与私钥都不存在时生成一套。
func LoadOrCreate(dir string) (*Authority, error) {
	dir = defaultDir(dir)
	certPath, keyPath, certExists, keyExists := resolvePaths(dir)
	switch {
	case certExists && keyExists:
		return load(dir, certPath, keyPath)
	case !certExists && !keyExists:
		return generate(dir, filepath.Join(dir, certFileName), keyPath)
	case certExists:
		return nil, fmt.Errorf("证书 %s 存在但私钥 %s 缺失", certPath, keyPath)
	default:
		return nil, fmt.Errorf("私钥 %s 存在但证书 %s 缺失", keyPath, certPath)
	}
}

// Load 加载已存在的 CA；缺失时返回错误，不会生成新 CA。
func Load(dir string) (*Authority, error) {
	dir = defaultDir(dir)
	certPath, keyPath, certExists, keyExists := resolvePaths(dir)
	if !certExists || !keyExists {
		return nil, fmt.Errorf("CA 不存在于 %s", dir)
	}
	return load(dir, certPath, keyPath)
}

// CertPath 返回已存在 CA 证书的路径，不读取私钥、不做规范化写入。
func CertPath(dir string) (string, error) {
	dir = defaultDir(dir)
	certPath, _, certExists, _ := resolvePaths(dir)
	if !certExists {
		return "", fmt.Errorf("CA 证书不存在于 %s", dir)
	}
	return certPath, nil
}

func defaultDir(dir string) string {
	if dir == "" {
		return DefaultDir
	}
	return dir
}

func resolvePaths(dir string) (certPath, keyPath string, certExists, keyExists bool) {
	dir = defaultDir(dir)
	certPath = filepath.Join(dir, certFileName)
	keyPath = filepath.Join(dir, keyFileName)
	certExists = fileExists(certPath)
	if !certExists {
		if legacy := filepath.Join(dir, legacyCertFileName); fileExists(legacy) {
			certPath, certExists = legacy, true
		}
	}
	return certPath, keyPath, certExists, fileExists(keyPath)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func generate(dir, certPath, keyPath string) (*Authority, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("创建证书目录 %s 失败: %w", dir, err)
	}
	_ = os.Chmod(dir, 0700)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("生成 CA 私钥失败: %w", err)
	}
	tmpl, err := caTemplate(time.Now(), &key.PublicKey)
	if err != nil {
		return nil, err
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("签发 CA 证书失败: %w", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("解析 CA 证书失败: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := writeFileAtomic(keyPath, keyPEM, 0600); err != nil {
		return nil, err
	}
	if err := writeFileAtomic(certPath, certPEM, 0644); err != nil {
		os.Remove(keyPath)
		return nil, err
	}
	return newAuthority(dir, certPath, keyPath, cert, key), nil
}

func load(dir, certPath, keyPath string) (*Authority, error) {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("读取私钥 %s 失败: %w", keyPath, err)
	}
	key, err := parsePrivateKey(keyData)
	if err != nil {
		return nil, fmt.Errorf("解析私钥 %s 失败: %w", keyPath, err)
	}
	certData, err := os.ReadFile(certPath)
	if err != nil {
		return nil, fmt.Errorf("读取证书 %s 失败: %w", certPath, err)
	}
	cert, err := parseCertificate(certData)
	if err != nil {
		return nil, fmt.Errorf("解析证书 %s 失败: %w", certPath, err)
	}
	if !cert.IsCA {
		return nil, fmt.Errorf("证书 %s 不是 CA", certPath)
	}
	if !publicKeysEqual(cert.PublicKey, &key.PublicKey) {
		return nil, fmt.Errorf("证书 %s 与私钥 %s 不匹配", certPath, keyPath)
	}
	if err := tightenPermissions(dir, keyPath); err != nil {
		return nil, err
	}
	// 旧版 .cer 或 DER 内容统一规范化为 PEM 的 fastgithub.crt，
	// 保证 update-ca-certificates 等工具拿到的是 PEM。
	canonical := filepath.Join(dir, certFileName)
	if certPath != canonical || !isPEMCertificate(certData) {
		certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert.Raw})
		if err := writeFileAtomic(canonical, certPEM, 0644); err != nil {
			return nil, err
		}
		certPath = canonical
	}
	return newAuthority(dir, certPath, keyPath, cert, key), nil
}

// tightenPermissions 收紧已存在 CA 的权限；Windows 上 chmod 语义有限，跳过。
func tightenPermissions(dir, keyPath string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return fmt.Errorf("收紧目录 %s 权限失败: %w", dir, err)
	}
	if err := os.Chmod(keyPath, 0600); err != nil {
		return fmt.Errorf("收紧私钥 %s 权限失败: %w", keyPath, err)
	}
	return nil
}

func newAuthority(dir, certPath, keyPath string, cert *x509.Certificate, key *rsa.PrivateKey) *Authority {
	return &Authority{
		dir:      dir,
		certPath: certPath,
		keyPath:  keyPath,
		cert:     cert,
		key:      key,
		now:      time.Now,
		leafs:    map[string]*tls.Certificate{},
	}
}

func caTemplate(now time.Time, pub *rsa.PublicKey) (*x509.Certificate, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("生成 CA 序列号失败: %w", err)
	}
	return &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "FastGithub"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLen:            1,
		SubjectKeyId:          subjectKeyID(pub),
		DNSNames:              []string{"FastGithub"},
	}, nil
}

// subjectKeyID 取公钥 SubjectPublicKeyInfo 的 SHA-1，与 Go 标准库生成
// CA 时的做法一致；这里仅作标识，不用于签名。
func subjectKeyID(pub *rsa.PublicKey) []byte {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil
	}
	sum := sha1.Sum(der)
	return sum[:]
}

// Certificate 返回 CA 证书。
func (a *Authority) Certificate() *x509.Certificate { return a.cert }

// CertPEM 返回 CA 证书的 PEM 编码。
func (a *Authority) CertPEM() []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: a.cert.Raw})
}

// CertPath 返回 CA 证书文件路径。
func (a *Authority) CertPath() string { return a.certPath }

// KeyPath 返回 CA 私钥文件路径。
func (a *Authority) KeyPath() string { return a.keyPath }
