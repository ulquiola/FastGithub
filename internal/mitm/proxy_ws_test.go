package mitm

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/dialer"
)

// TestWebSocketThroughCONNECT 走完整的 CONNECT → MITM → Upgrade 透传链路：
// 客户端只通告 http/1.1（浏览器建立 WebSocket 时就是这样），h2 开关保持开启。
func TestWebSocketThroughCONNECT(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			http.Error(w, "missing upgrade", http.StatusBadRequest)
			return
		}
		conn, brw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		buf := make([]byte, 5)
		if _, err := io.ReadFull(brw, buf); err != nil {
			return
		}
		conn.Write(buf)
	}))
	defer up.Close()

	cfg := config.Default()
	cfg.Domains = map[string]*config.Domain{"example.com": {Destination: up.URL + "/"}}
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := certs.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	d := dialer.New(store, nil, nil)
	reverse := NewReverseProxy(store, dialer.NewTransport(store, d, dialer.WithHTTP2(true)))
	reverse.SetUpgradeTransport(dialer.NewTransport(store, d))
	s := NewServer(store, d, ca, reverse, time.Minute)
	s.SetHTTP2(true)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
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
			go s.ServeConn(ctx, conn)
		}
	}()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))

	fmt.Fprintf(conn, "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n")
	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil || !strings.Contains(status, "200") {
		t.Fatalf("CONNECT 响应 %q err=%v", status, err)
	}
	readHeaders(t, br)

	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate())
	// 只通告 http/1.1：WebSocket 的 101 升级不是 h2 的扩展 CONNECT。
	tlsConn := tls.Client(&bufferedConn{Conn: conn, r: br}, &tls.Config{
		RootCAs:    roots,
		ServerName: "example.com",
		NextProtos: []string{"http/1.1"},
	})
	if err := tlsConn.Handshake(); err != nil {
		t.Fatalf("TLS 握手: %v", err)
	}
	fmt.Fprintf(tlsConn, "GET /ws HTTP/1.1\r\nHost: example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
	tbr := bufio.NewReader(tlsConn)
	status, err = tbr.ReadString('\n')
	if err != nil || !strings.Contains(status, "101") {
		t.Fatalf("升级响应 %q err=%v", status, err)
	}
	readHeaders(t, tbr)

	if _, err := tlsConn.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(tbr, buf); err != nil || string(buf) != "hello" {
		t.Fatalf("升级后数据 %q err=%v", buf, err)
	}
}

func readHeaders(t *testing.T, br *bufio.Reader) {
	t.Helper()
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("读取响应头: %v", err)
		}
		if line == "\r\n" {
			return
		}
	}
}
