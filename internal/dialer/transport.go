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

// NewTransport 返回所有出站连接都经过 dialer 的 HTTP Transport。
func NewTransport(store *config.Store, d *Dialer) *http.Transport {
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
	return &http.Transport{
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
			return d.DialTLS(ctx, t)
		},
		ForceAttemptHTTP2:   false,
		TLSNextProto:        map[string]func(string, *tls.Conn) http.RoundTripper{},
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
	}
}
