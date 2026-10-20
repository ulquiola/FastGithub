package app

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"fastgithub-go/internal/config"
)

func startTestServe(t *testing.T, cfg *config.Config, store *config.Store) (proxyAddr, uiAddr string, cancel func(), done <-chan error) {
	t.Helper()
	proxyLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	uiLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		proxyLn.Close()
		t.Fatal(err)
	}
	ctx, cancelCtx := context.WithCancel(context.Background())
	ch := make(chan error, 1)
	go func() { ch <- serve(ctx, cfg, store, proxyLn, uiLn, t.TempDir(), nil) }()
	return proxyLn.Addr().String(), uiLn.Addr().String(), cancelCtx, ch
}

func waitServe(t *testing.T, done <-chan error) {
	t.Helper()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve 退出: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 Serve 未退出")
	}
}

func TestServePACAndUI(t *testing.T) {
	cfg := config.Default()
	cfg.Domains = map[string]*config.Domain{"github.com": {}}
	cfg.Log.File = filepath.Join(t.TempDir(), "fastgithub.log")
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	store.Lookup("github.com")
	proxyAddr, uiAddr, cancel, done := startTestServe(t, cfg, store)
	defer cancel()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + proxyAddr + "/")
	if err != nil {
		t.Fatalf("GET PAC: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "FindProxyForURL") || !strings.Contains(string(body), "github.com") {
		t.Fatalf("PAC = %s", body)
	}

	resp, err = client.Get("http://" + uiAddr + "/")
	if err != nil {
		t.Fatalf("GET UI: %v", err)
	}
	body, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(body), "FastGithub") {
		t.Fatalf("UI = %s", body)
	}

	resp, err = client.Get("http://" + uiAddr + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	var status map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if status["proxyPort"].(float64) != float64(cfg.Server.ProxyPort) {
		t.Fatalf("status = %v", status)
	}

	resp, err = client.Get("http://" + proxyAddr + "/flowStatistics")
	if err != nil {
		t.Fatal(err)
	}
	var stats map[string]int64
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if _, ok := stats["TotalRead"]; !ok {
		t.Fatalf("flowStatistics = %v", stats)
	}

	cancel()
	waitServe(t, done)
}

func TestServeShutdownClosesActiveConn(t *testing.T) {
	cfg := config.Default()
	cfg.Domains = map[string]*config.Domain{"github.com": {}}
	cfg.Log.File = filepath.Join(t.TempDir(), "fastgithub.log")
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	proxyAddr, _, cancel, done := startTestServe(t, cfg, store)

	conn, err := net.Dial("tcp", proxyAddr)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\n\r\n", proxyAddr)
	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		cancel()
		t.Fatalf("PAC 响应: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	cancel()
	waitServe(t, done)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := br.ReadByte(); err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			t.Fatal("shutdown 未关闭已建立的活动连接")
		}
	}
}

func TestServeUIDomains(t *testing.T) {
	cfg := config.Default()
	cfg.Domains = map[string]*config.Domain{"github.com": {}}
	cfg.Log.File = filepath.Join(t.TempDir(), "fastgithub.log")
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	store.Lookup("github.com")
	_, uiAddr, cancel, done := startTestServe(t, cfg, store)
	defer cancel()

	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + uiAddr + "/api/domains")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var domains []struct {
		Pattern string `json:"pattern"`
		Hits    int64  `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&domains); err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0].Pattern != "github.com" || domains[0].Hits < 1 {
		t.Fatalf("domains = %+v", domains)
	}
	cancel()
	waitServe(t, done)
}
