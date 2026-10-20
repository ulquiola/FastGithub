package app

import (
	"context"
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
	go func() { done <- Serve(ctx, cfg, store, ln, t.TempDir()) }()

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
