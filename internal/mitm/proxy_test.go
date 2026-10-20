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
	"sync"
	"testing"
	"time"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/dialer"
	"fastgithub-go/internal/flow"
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
	var mu sync.Mutex
	conns := map[net.Conn]struct{}{}
	t.Cleanup(func() {
		cancel()
		ln.Close()
		mu.Lock()
		for conn := range conns {
			conn.Close()
		}
		mu.Unlock()
	})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns[conn] = struct{}{}
			mu.Unlock()
			go func(conn net.Conn) {
				defer func() {
					mu.Lock()
					delete(conns, conn)
					mu.Unlock()
				}()
				server.ServeConn(ctx, conn)
			}(conn)
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

type loopbackResolver struct{}

func (loopbackResolver) Lookup(context.Context, string) ([]net.IP, error) {
	return []net.IP{net.ParseIP("127.0.0.1")}, nil
}

func TestServerTunnelKeepsPipelinedBytes(t *testing.T) {
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
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	// CONNECT 与 payload 一次写入，验证 bufio 预读的字节没有丢失。
	fmt.Fprintf(conn, "CONNECT 127.0.0.1:%d HTTP/1.1\r\nHost: x\r\n\r\nXYZ", echoLn.Addr().(*net.TCPAddr).Port)
	br := bufio.NewReader(conn)
	if _, err := http.ReadResponse(br, nil); err != nil {
		t.Fatalf("CONNECT 响应: %v", err)
	}
	buf := make([]byte, 3)
	if _, err := io.ReadFull(br, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "XYZ" {
		t.Fatalf("预读字节丢失，got %q", buf)
	}
}

func TestServerTransparentTLS(t *testing.T) {
	server, ca := newTestServer(t, map[string]*config.Domain{"example.com": {}})
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate())

	serverSide, clientSide := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go server.ServeConn(ctx, serverSide)

	tlsConn := tls.Client(clientSide, &tls.Config{ServerName: "example.com", RootCAs: roots})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatalf("透明 TLS 握手失败: %v", err)
	}
	tlsConn.Close()
}

func TestServerDestinationSNI(t *testing.T) {
	ca, err := certs.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	destLeaf, err := ca.LeafFor("fonts.geekzu.org")
	if err != nil {
		t.Fatal(err)
	}
	sniCh := make(chan string, 1)
	upstream := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sniCh <- r.TLS.ServerName
		io.WriteString(w, "ok")
	}))
	upstream.TLS = &tls.Config{Certificates: []tls.Certificate{*destLeaf}}
	upstream.StartTLS()
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.Default()
	cfg.Domains = map[string]*config.Domain{
		"fonts.googleapis.com": {Destination: "https://fonts.geekzu.org:" + upstreamURL.Port() + "/"},
	}
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate())
	d := dialer.New(store, loopbackResolver{}, roots)
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
	resp, err := client.Get("https://fonts.googleapis.com:" + upstreamURL.Port() + "/css")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
	if sni := <-sniCh; sni != "fonts.geekzu.org" {
		t.Fatalf("上游 SNI = %q，期望 destination host", sni)
	}
}

type captureLogger struct {
	entries chan flow.Entry
}

func newCaptureLogger() *captureLogger {
	return &captureLogger{entries: make(chan flow.Entry, 8)}
}

func (l *captureLogger) Log(e flow.Entry) { l.entries <- e }

func TestReverseProxyLogging(t *testing.T) {
	store := testStore(t, map[string]*config.Domain{"example.com": {}})
	logger := newCaptureLogger()
	rp := NewReverseProxy(store, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: 200,
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("hello:" + string(body))),
		}, nil
	}))
	rp.SetLogger(logger)
	req := httptest.NewRequest(http.MethodPost, "https://example.com/x", strings.NewReader("abc"))
	req.Host = "example.com"
	rp.ServeHTTP(httptest.NewRecorder(), req)

	select {
	case e := <-logger.entries:
		if e.Domain != "example.com" || e.Method != http.MethodPost || e.Status != 200 {
			t.Fatalf("entry = %+v", e)
		}
		if e.Read != 3 || e.Write != int64(len("hello:abc")) {
			t.Fatalf("bytes = %d/%d", e.Read, e.Write)
		}
	case <-time.After(time.Second):
		t.Fatal("未记录日志")
	}
}

func TestTunnelLogging(t *testing.T) {
	server, _ := newTestServer(t, map[string]*config.Domain{})
	logger := newCaptureLogger()
	server.SetLogger(logger)
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
	fmt.Fprintf(conn, "CONNECT 127.0.0.1:%d HTTP/1.1\r\nHost: x\r\n\r\n", echoLn.Addr().(*net.TCPAddr).Port)
	br := bufio.NewReader(conn)
	if _, err := http.ReadResponse(br, nil); err != nil {
		t.Fatal(err)
	}
	conn.Write([]byte("hi"))
	buf := make([]byte, 2)
	if _, err := io.ReadFull(br, buf); err != nil {
		t.Fatal(err)
	}
	conn.Close()

	select {
	case e := <-logger.entries:
		if e.Method != "CONNECT" || e.Domain != "127.0.0.1" || e.Read < 2 || e.Write < 2 {
			t.Fatalf("entry = %+v", e)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("未记录隧道日志")
	}
}

func TestFlowStatisticsHandler(t *testing.T) {
	server, _ := newTestServer(t, map[string]*config.Domain{})
	server.SetStats(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"TotalRead":1,"TotalWrite":2,"ReadRate":0,"WriteRate":0}`)
	}))
	proxyAddr := startProxy(t, server)
	resp, err := http.Get("http://" + proxyAddr + "/flowStatistics")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"TotalRead":1`) {
		t.Fatalf("body = %s", body)
	}
}
