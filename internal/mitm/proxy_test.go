package mitm

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/dialer"
)

type staticResolver struct{}

func (staticResolver) Lookup(context.Context, string) ([]net.IP, error) {
	return nil, errors.New("测试不解析")
}

func newTestServer(t *testing.T, domains map[string]*config.Domain) (*Server, *certs.Authority) {
	t.Helper()
	cfg := config.Default()
	cfg.Domains = domains
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := certs.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate())
	d := dialer.New(store, staticResolver{}, roots)
	transport := dialer.NewTransport(store, d)
	reverse := NewReverseProxy(store, transport)
	return NewServer(store, d, ca, reverse, time.Minute), ca
}

func startProxy(t *testing.T, server *Server) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		ln.Close()
	})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go server.ServeConn(ctx, conn)
		}
	}()
	return ln.Addr().String()
}

func TestServerMITMEndToEnd(t *testing.T) {
	ca, err := certs.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := ca.LeafFor("example.com")
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "example.com" && !strings.HasPrefix(r.Host, "example.com:") {
			t.Errorf("上游 Host = %q", r.Host)
		}
		io.WriteString(w, "hello")
	}))
	upstream.TLS = &tls.Config{Certificates: []tls.Certificate{*leaf}}
	upstream.StartTLS()
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Domains = map[string]*config.Domain{"example.com": {IP: "127.0.0.1"}}
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate())
	d := dialer.New(store, staticResolver{}, roots)
	server := NewServer(store, d, ca, NewReverseProxy(store, dialer.NewTransport(store, d)), time.Minute)
	proxyAddr := startProxy(t, server)

	proxyURL, err := url.Parse("http://" + proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{
		Proxy:           http.ProxyURL(proxyURL),
		TLSClientConfig: &tls.Config{RootCAs: roots},
	}}
	resp, err := client.Get("https://example.com:" + upstreamURL.Port() + "/hello")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK || string(body) != "hello" {
		t.Fatalf("响应 = %d %q", resp.StatusCode, body)
	}
}

func TestServerTunnel(t *testing.T) {
	server, _ := newTestServer(t, map[string]*config.Domain{})
	proxyAddr := startProxy(t, server)

	echoLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echoLn.Close()
	go func() {
		conn, err := echoLn.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	echoPort := echoLn.Addr().(*net.TCPAddr).Port
	fmt.Fprintf(conn, "CONNECT 127.0.0.1:%d HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n", echoPort)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("CONNECT 响应: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT 状态 = %d", resp.StatusCode)
	}
	if _, err := conn.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 2)
	if _, err := io.ReadFull(br, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hi" {
		t.Fatalf("echo = %q", buf)
	}
}

func TestServerPAC(t *testing.T) {
	server, _ := newTestServer(t, map[string]*config.Domain{
		"github.com": {},
	})
	proxyAddr := startProxy(t, server)
	resp, err := http.Get("http://" + proxyAddr + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "FindProxyForURL") || !strings.Contains(text, "PROXY "+proxyAddr) {
		t.Fatalf("PAC = %s", text)
	}
	if !strings.Contains(text, "shExpMatch(host, 'github.com')") {
		t.Fatalf("PAC 缺少规则: %s", text)
	}
}
