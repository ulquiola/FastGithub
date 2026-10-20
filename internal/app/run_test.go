package app

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"fastgithub-go/internal/config"
)

func TestServePACAndShutdown(t *testing.T) {
	cfg := config.Default()
	cfg.Domains = map[string]*config.Domain{"github.com": {}}
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg, store, ln, t.TempDir(), nil) }()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + ln.Addr().String() + "/")
	if err != nil {
		cancel()
		t.Fatalf("GET PAC: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "FindProxyForURL") || !strings.Contains(string(body), "github.com") {
		cancel()
		t.Fatalf("PAC = %s", body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve 退出: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 Serve 未退出")
	}
}

func TestServeShutdownClosesActiveConn(t *testing.T) {
	cfg := config.Default()
	cfg.Domains = map[string]*config.Domain{"github.com": {}}
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg, store, ln, t.TempDir(), nil) }()

	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\n\r\n", ln.Addr())
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		cancel()
		t.Fatalf("PAC 响应: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve 退出: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 Serve 未退出")
	}

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := br.ReadByte(); err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			t.Fatal("shutdown 未关闭已建立的活动连接")
		}
	}
}
