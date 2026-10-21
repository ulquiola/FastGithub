package mitm

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/dialer"
)

// benchmarkMITM 测量完整 CONNECT → MITM → 上游链路的单请求开销。
func benchmarkMITM(b *testing.B, http2, parallel bool) {
	b.Helper()
	up := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello")
	}))
	up.EnableHTTP2 = http2
	up.StartTLS()
	defer up.Close()

	cfg := config.Default()
	cfg.Domains = map[string]*config.Domain{"example.com": {Destination: up.URL + "/"}}
	store, err := config.NewStore(cfg)
	if err != nil {
		b.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(up.Certificate())
	d := dialer.New(store, nil, roots)
	reverse := NewReverseProxy(store, dialer.NewTransport(store, d, dialer.WithHTTP2(http2)))
	ca, err := certs.LoadOrCreate(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	s := NewServer(store, d, ca, reverse, time.Minute)
	s.SetHTTP2(http2)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer ln.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serveMITM(ctx, conn, "example.com")
		}
	}()

	caRoots := x509.NewCertPool()
	caRoots.AddCert(ca.Certificate())
	clientTransport := &http.Transport{
		TLSClientConfig:     &tls.Config{RootCAs: caRoots, ServerName: "example.com"},
		ForceAttemptHTTP2:   http2,
		MaxIdleConns:        64,
		MaxIdleConnsPerHost: 64,
	}
	if !http2 {
		clientTransport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}
	client := &http.Client{Transport: clientTransport}
	defer client.CloseIdleConnections()
	url := "https://" + ln.Addr().String() + "/"
	do := func() error {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		// 让反向代理命中 example.com，再按 destination 转发到本地上游。
		req.Host = "example.com"
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		_, err = io.Copy(io.Discard, resp.Body)
		return err
	}
	if err := do(); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	if parallel {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				if err := do(); err != nil {
					b.Fatal(err)
				}
			}
		})
		return
	}
	for i := 0; i < b.N; i++ {
		if err := do(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMITMHTTP1(b *testing.B) { benchmarkMITM(b, false, false) }
func BenchmarkMITMHTTP2(b *testing.B) { benchmarkMITM(b, true, false) }

func BenchmarkMITMHTTP1Parallel(b *testing.B) { benchmarkMITM(b, false, true) }
func BenchmarkMITMHTTP2Parallel(b *testing.B) { benchmarkMITM(b, true, true) }
