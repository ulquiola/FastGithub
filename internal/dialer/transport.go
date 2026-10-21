package dialer

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strconv"
	"time"

	"fastgithub-go/internal/config"
)

type transportOptions struct {
	http2 bool
}

// TransportOption 调整出站 transport 的协议栈。
type TransportOption func(*transportOptions)

// WithHTTP2 决定回源是否协商 HTTP/2。
func WithHTTP2(enabled bool) TransportOption {
	return func(o *transportOptions) { o.http2 = enabled }
}

// NewTransport 返回所有出站连接都经过 dialer 的 HTTP Transport。
// 默认保持 v0.3.6 行为：不协商 h2。
func NewTransport(store *config.Store, d *Dialer, opts ...TransportOption) *http.Transport {
	var o transportOptions
	for _, opt := range opts {
		opt(&o)
	}
	target := func(addr string) (Target, error) {
		host, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return Target{}, err
		}
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return Target{}, err
		}
		domain, _ := store.Lookup(host)
		return Target{Host: host, Port: port, Domain: domain}, nil
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			t, err := target(addr)
			if err != nil {
				return nil, err
			}
			return d.DialTCP(ctx, t)
		},
		DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			t, err := target(addr)
			if err != nil {
				return nil, err
			}
			if o.http2 {
				return d.DialTLSH2(ctx, t)
			}
			return d.DialTLS(ctx, t)
		},
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	}
	if o.http2 {
		tr.ForceAttemptHTTP2 = true
	} else {
		// 自定义 DialTLSContext 下，空 TLSNextProto 是停用 h2 的唯一可靠方式。
		tr.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}
	return tr
}
