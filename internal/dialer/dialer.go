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

const connectTimeout = 10 * time.Second

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
	store          *config.Store
	resolver       Resolver
	roots          *x509.CertPool
	dialer         net.Dialer
	connectTimeout time.Duration
}

// New 创建 dialer；roots 为空时使用系统根证书。
func New(store *config.Store, resolver Resolver, roots *x509.CertPool) *Dialer {
	if roots == nil {
		if pool, err := x509.SystemCertPool(); err == nil {
			roots = pool
		}
	}
	return &Dialer{
		store:    store,
		resolver: resolver,
		roots:    roots,
		// 超时由每次尝试的 context 控制，Domain.Timeout 才能突破默认 10s。
		dialer:         net.Dialer{},
		connectTimeout: connectTimeout,
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
	// 没有域名规则时（例如 Destination 改写后的镜像主机）默认发送真实主机名。
	sni := t.Host
	ignoreName := false
	if t.Domain != nil {
		sni = config.ExpandSNI(t.Domain.SNIPattern(), t.Host, ip)
		ignoreName = t.Domain.TLSIgnoreNameMismatch
	}
	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			return verifyParsed(cs.PeerCertificates, t.Host, ignoreName, d.roots)
		},
		MinVersion: tls.VersionTLS12,
	})
	handshakeCtx, cancel := context.WithTimeout(ctx, d.timeoutFor(t))
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
	timeout := d.timeoutFor(t)
	var errs []error
	for _, ip := range ips {
		attemptCtx, cancel := context.WithTimeout(ctx, timeout)
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

// timeoutFor 返回该域名的连接/握手预算；Domain.Timeout 为零时用默认 10s。
func (d *Dialer) timeoutFor(t Target) time.Duration {
	if t.Domain != nil {
		if timeout := t.Domain.Timeout.Std(); timeout > 0 {
			return timeout
		}
	}
	return d.connectTimeout
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
