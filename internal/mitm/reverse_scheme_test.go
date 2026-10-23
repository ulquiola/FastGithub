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

// WebKit 通过 HTTP 代理发 WebSocket 时使用绝对 URI（GET ws://host/path）。
// 反向代理必须把它归一化成 http/https，否则 http.Transport 会拒绝 ws 方案。
func TestReverseProxyNormalizesWebSocketScheme(t *testing.T) {
	cases := []struct {
		scheme string
		want   string
	}{
		{"ws", "http"},
		{"wss", "https"},
	}
	for _, tc := range cases {
		t.Run(tc.scheme, func(t *testing.T) {
			store, err := config.NewStore(config.Default())
			if err != nil {
				t.Fatal(err)
			}
			var upstreamScheme string
			rp := NewReverseProxy(store, roundTripFunc(func(r *http.Request) (*http.Response, error) {
				upstreamScheme = r.URL.Scheme
				pr, pw := io.Pipe()
				go func() {
					pw.Write([]byte("ok"))
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
			fmt.Fprintf(conn, "GET %s://example.com/ws HTTP/1.1\r\nHost: example.com\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", tc.scheme)

			br := bufio.NewReader(conn)
			status, err := br.ReadString('\n')
			if err != nil || !strings.Contains(status, "101") {
				t.Fatalf("升级响应 %q err=%v", status, err)
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
			buf := make([]byte, 2)
			if _, err := io.ReadFull(br, buf); err != nil || string(buf) != "ok" {
				t.Fatalf("升级后数据 %q err=%v", buf, err)
			}
			if upstreamScheme != tc.want {
				t.Fatalf("上游 scheme = %q，期望 %q", upstreamScheme, tc.want)
			}
		})
	}
}
