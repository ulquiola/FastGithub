package certs

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

const (
	maxLeafCache    = 1024
	leafRenewBefore = 24 * time.Hour
	leafLifetime    = 365 * 24 * time.Hour
)

// LeafFor 返回 domain 对应的叶子证书，命中缓存时直接复用。
func (a *Authority) LeafFor(domain string) (*tls.Certificate, error) {
	domain = strings.ToLower(strings.TrimSuffix(domain, "."))
	if domain == "" {
		return nil, fmt.Errorf("域名不能为空")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now := a.now()
	if c, ok := a.leafs[domain]; ok && c.Leaf != nil && now.Add(leafRenewBefore).Before(c.Leaf.NotAfter) {
		return c, nil
	}
	c, err := a.newLeaf(domain, now)
	if err != nil {
		return nil, err
	}
	if len(a.leafs) >= maxLeafCache {
		a.sweepLocked(now)
		if len(a.leafs) >= maxLeafCache {
			a.leafs = map[string]*tls.Certificate{}
		}
	}
	a.leafs[domain] = c
	return c, nil
}

func (a *Authority) newLeaf(domain string, now time.Time) (*tls.Certificate, error) {
	if !now.Before(a.cert.NotAfter.Add(-leafRenewBefore)) {
		return nil, fmt.Errorf("CA 证书 %s 即将过期（%s），请删除后重新生成", a.certPath, a.cert.NotAfter.Format(time.RFC3339))
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, fmt.Errorf("生成 %s 的私钥失败: %w", domain, err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, fmt.Errorf("生成序列号失败: %w", err)
	}
	notAfter := now.Add(leafLifetime)
	if notAfter.After(a.cert.NotAfter) {
		notAfter = a.cert.NotAfter
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: domain},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		SubjectKeyId:          subjectKeyID(&key.PublicKey),
		AuthorityKeyId:        a.cert.SubjectKeyId,
	}
	addSAN(tmpl, domain)
	der, err := x509.CreateCertificate(rand.Reader, tmpl, a.cert, &key.PublicKey, a.key)
	if err != nil {
		return nil, fmt.Errorf("签发 %s 的证书失败: %w", domain, err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("解析 %s 的证书失败: %w", domain, err)
	}
	return &tls.Certificate{
		Certificate: [][]byte{der, a.cert.Raw},
		PrivateKey:  key,
		Leaf:        leaf,
	}, nil
}

func addSAN(t *x509.Certificate, domain string) {
	if ip := net.ParseIP(domain); ip != nil {
		t.IPAddresses = append(t.IPAddresses, ip)
	} else {
		t.DNSNames = append(t.DNSNames, domain)
	}
	if host, err := os.Hostname(); err == nil && validHostname(host) && host != domain {
		t.DNSNames = append(t.DNSNames, host)
	}
	for _, raw := range []string{"127.0.0.1", "::1"} {
		ip := net.ParseIP(raw)
		if !containsIP(t.IPAddresses, ip) {
			t.IPAddresses = append(t.IPAddresses, ip)
		}
	}
}

// validHostname 只接受 IA5 字符，避免非 ASCII 主机名让证书签发失败。
func validHostname(host string) bool {
	if host == "" {
		return false
	}
	for i := 0; i < len(host); i++ {
		c := host[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9', c == '-', c == '.':
		default:
			return false
		}
	}
	return true
}

func containsIP(ips []net.IP, want net.IP) bool {
	for _, ip := range ips {
		if ip.Equal(want) {
			return true
		}
	}
	return false
}

// sweepLocked 清理已过期的缓存；调用方必须持有 a.mu。
func (a *Authority) sweepLocked(now time.Time) {
	for domain, c := range a.leafs {
		if c.Leaf == nil || !now.Before(c.Leaf.NotAfter) {
			delete(a.leafs, domain)
		}
	}
}
