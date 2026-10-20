package dialer

import (
	"crypto/x509"
	"fmt"
	"net"
	"strings"
)

// verifyPeer 先验证证书链，再按原版规则校验名称。
func verifyPeer(rawCerts [][]byte, host string, ignoreName bool, roots *x509.CertPool) error {
	if len(rawCerts) == 0 {
		return fmt.Errorf("服务器未提供证书")
	}
	certs := make([]*x509.Certificate, len(rawCerts))
	for i, raw := range rawCerts {
		cert, err := x509.ParseCertificate(raw)
		if err != nil {
			return fmt.Errorf("解析服务器证书失败: %w", err)
		}
		certs[i] = cert
	}
	intermediates := x509.NewCertPool()
	for _, cert := range certs[1:] {
		intermediates.AddCert(cert)
	}
	if _, err := certs[0].Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return fmt.Errorf("证书链校验失败: %w", err)
	}
	if ignoreName {
		return nil
	}
	if !matchName(certs[0], host) {
		return fmt.Errorf("证书名称不匹配: %s", host)
	}
	return nil
}

// matchName 复刻原版判定：精确相等，或 "*" 前缀且后缀匹配。
func matchName(cert *x509.Certificate, host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if ip := net.ParseIP(host); ip != nil {
		for _, certIP := range cert.IPAddresses {
			if certIP.Equal(ip) {
				return true
			}
		}
		return false
	}
	for _, name := range cert.DNSNames {
		name = strings.ToLower(name)
		if name == host {
			return true
		}
		if strings.HasPrefix(name, "*") && strings.HasSuffix(host, name[1:]) {
			return true
		}
	}
	return false
}
