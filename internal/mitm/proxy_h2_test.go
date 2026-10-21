package mitm

import (
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	"fastgithub-go/internal/certs"
)

func startOneTLS(t *testing.T, s *Server) (net.Listener, *x509.CertPool) {
	t.Helper()
	ca, err := certs.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.ca = ca
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		s.serveOneTLS(conn, s.serverTLSConfig("example.com"), s.reverse)
	}()
	roots := x509.NewCertPool()
	roots.AddCert(ca.Certificate())
	return ln, roots
}

func TestServeOneTLSServesHTTP2(t *testing.T) {
	seen := make(chan string, 1)
	s := NewServer(nil, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Proto
		io.WriteString(w, "ok")
	}), time.Minute)
	s.SetHTTP2(true)
	ln, roots := startOneTLS(t, s)

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: roots, ServerName: "example.com"},
		ForceAttemptHTTP2: true,
	}}
	defer client.CloseIdleConnections()
	resp, err := client.Get("https://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.ProtoMajor != 2 {
		t.Fatalf("客户端协议 %s，期望 h2", resp.Proto)
	}
	if proto := <-seen; proto != "HTTP/2.0" {
		t.Fatalf("handler 看到 %s", proto)
	}
}

func TestServeOneTLSFallsBackToHTTP1(t *testing.T) {
	seen := make(chan string, 1)
	s := NewServer(nil, nil, nil, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Proto
		io.WriteString(w, "ok")
	}), time.Minute)
	s.SetHTTP2(false)
	ln, roots := startOneTLS(t, s)

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: roots, ServerName: "example.com"},
		ForceAttemptHTTP2: false,
	}}
	defer client.CloseIdleConnections()
	resp, err := client.Get("https://" + ln.Addr().String() + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.ProtoMajor != 1 {
		t.Fatalf("客户端协议 %s，期望 h1", resp.Proto)
	}
	if proto := <-seen; proto != "HTTP/1.1" {
		t.Fatalf("handler 看到 %s", proto)
	}
}
