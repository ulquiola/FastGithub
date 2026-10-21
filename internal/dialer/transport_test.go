package dialer

import (
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"

	"fastgithub-go/internal/config"
)

// h2Upstream 启动一个支持 h2 的 TLS 上游，并把每个请求的协议写入 seen。
func h2Upstream(t *testing.T, seen chan<- string) *httptest.Server {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Proto
		w.Write([]byte("ok"))
	}))
	srv.EnableHTTP2 = true
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

func mustRequest(t *testing.T, rawurl string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, rawurl, nil)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestTransportHTTP2ToUpstream(t *testing.T) {
	seen := make(chan string, 1)
	up := h2Upstream(t, seen)
	pool := x509.NewCertPool()
	pool.AddCert(up.Certificate())
	store, err := config.NewStore(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	d := New(store, nil, pool)
	tr := NewTransport(store, d, WithHTTP2(true))
	resp, err := tr.RoundTrip(mustRequest(t, up.URL))
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	resp.Body.Close()
	if proto := <-seen; proto != "HTTP/2.0" {
		t.Fatalf("上游看到 %s，期望 HTTP/2.0", proto)
	}
}

func TestTransportHTTP2Disabled(t *testing.T) {
	seen := make(chan string, 1)
	up := h2Upstream(t, seen)
	pool := x509.NewCertPool()
	pool.AddCert(up.Certificate())
	store, err := config.NewStore(config.Default())
	if err != nil {
		t.Fatal(err)
	}
	d := New(store, nil, pool)
	tr := NewTransport(store, d)
	if tr.ForceAttemptHTTP2 {
		t.Fatal("默认不应启用 h2")
	}
	if tr.TLSNextProto == nil {
		t.Fatal("默认必须保留空 TLSNextProto 以关闭 h2")
	}
	resp, err := tr.RoundTrip(mustRequest(t, up.URL))
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	resp.Body.Close()
	if proto := <-seen; proto != "HTTP/1.1" {
		t.Fatalf("上游看到 %s，期望 HTTP/1.1", proto)
	}
}
