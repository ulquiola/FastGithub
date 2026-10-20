package dialer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"fastgithub-go/internal/config"
)

const (
	connectTimeout   = 10 * time.Second
	handshakeTimeout = 10 * time.Second
)

// Resolver 是 dialer 需要的域名解析能力。
type Resolver interface {
	Lookup(ctx context.Context, domain string) ([]net.IP, error)
}

// Target 是一次上游连接的目标。
type Target struct {
	Host   string
	Port   int
	Domain *config.Domain
}

// Dialer 按域名规则选择 IP、构造 SNI 并校验上游证书。
type Dialer struct {
	store            *config.Store
	resolver         Resolver
	roots            *x509.CertPool
	dialer           net.Dialer
	connectTimeout   time.Duration
	handshakeTimeout time.Duration
}

// New 创建 dialer；roots 为空时使用系统根证书。
func New(store *config.Store, resolver Resolver, roots *x509.CertPool) *Dialer {
	if roots == nil {
		if pool, err := x509.SystemCertPool(); err == nil {
			roots = pool
		}
	}
	return &Dialer{
		store:            store,
		resolver:         resolver,
		roots:            roots,
		dialer:           net.Dialer{Timeout: connectTimeout},
		connectTimeout:   connectTimeout,
		handshakeTimeout: handshakeTimeout,
	}
}

// DialTCP 按 IP 优先级建立明文连接。
func (d *Dialer) DialTCP(ctx context.Context, t Target) (net.Conn, error) {
	conn, _, err := d.dialFirst(ctx, t)
	return conn, err
}

// DialTLS 建立 TLS 连接；SNI 与证书判定遵循域名规则。
func (d *Dialer) DialTLS(ctx context.Context, t Target) (*tls.Conn, error) {
	conn, ip, err := d.dialFirst(ctx, t)
	if err != nil {
		return nil, err
	}
	sni := ""
	ignoreName := false
	if t.Domain != nil {
		sni = config.ExpandSNI(t.Domain.SNIPattern(), t.Host, ip)
		ignoreName = t.Domain.TLSIgnoreNameMismatch
	}
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
			return verifyPeer(rawCerts, t.Host, ignoreName, d.roots)
		},
		MinVersion: tls.VersionTLS12,
	})
	handshakeCtx, cancel := context.WithTimeout(ctx, d.handshakeTimeout)
	defer cancel()
	if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
		conn.Close()
		return nil, err
	}
	return tlsConn, nil
}

func (d *Dialer) dialFirst(ctx context.Context, t Target) (net.Conn, net.IP, error) {
	ips, err := d.ips(ctx, t)
	if err != nil {
		return nil, nil, err
	}
	var errs []error
	for _, ip := range ips {
		attemptCtx, cancel := context.WithTimeout(ctx, d.connectTimeout)
		conn, err := d.dialer.DialContext(attemptCtx, "tcp", net.JoinHostPort(ip.String(), strconv.Itoa(t.Port)))
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", ip, err))
			continue
		}
		return conn, ip, nil
	}
	return nil, nil, errors.Join(errs...)
}

func (d *Dialer) ips(ctx context.Context, t Target) ([]net.IP, error) {
	var out []net.IP
	if t.Domain != nil && t.Domain.IP != "" {
		if ip := net.ParseIP(t.Domain.IP); ip != nil {
			out = append(out, ip)
		}
	}
	if ip := net.ParseIP(t.Host); ip != nil {
		out = append(out, ip)
	} else {
		ips, err := d.resolver.Lookup(ctx, t.Host)
		if err != nil && len(out) == 0 {
			return nil, err
		}
		out = append(out, ips...)
	}
	out = dedupIPs(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("没有可用 IP: %s", t.Host)
	}
	return out, nil
}

func dedupIPs(ips []net.IP) []net.IP {
	seen := make(map[string]bool, len(ips))
	out := ips[:0]
	for _, ip := range ips {
		key := ip.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ip)
	}
	return out
}
