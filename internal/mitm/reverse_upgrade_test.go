package mitm

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fastgithub-go/internal/config"
)

type upgradeBody struct {
	io.Reader
	io.Writer
}

func (upgradeBody) Close() error { return nil }

func TestReverseProxyPassesUpgrade(t *testing.T) {
	store, err := config.NewStore(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	rp := NewReverseProxy(store, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Upgrade") != "websocket" {
			t.Errorf("Upgrade 头被丢弃: %v", r.Header)
		}
		if !strings.EqualFold(r.Header.Get("Connection"), "Upgrade") {
			t.Errorf("Connection 头 = %q", r.Header.Get("Connection"))
		}
		pr, pw := io.Pipe()
		go func() {
			pw.Write([]byte("hello"))
			pw.Close()
		}()
		return &http.Response{
			StatusCode: http.StatusSwitchingProtocols,
			Header:     http.Header{"Upgrade": {"websocket"}, "Connection": {"Upgrade"}},
			Body:       upgradeBody{Reader: pr, Writer: io.Discard},
		}, nil
	}))

	srv := httptest.NewServer(rp)
	defer srv.Close()
	conn, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET /ws HTTP/1.1\r\nHost: example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")

	br := bufio.NewReader(conn)
	status, err := br.ReadString('\n')
	if err != nil || !strings.Contains(status, "101") {
		t.Fatalf("状态行 %q err=%v", status, err)
	}
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if line == "\r\n" {
			break
		}
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(br, buf); err != nil || string(buf) != "hello" {
		t.Fatalf("升级后数据 %q err=%v", buf, err)
	}
}
