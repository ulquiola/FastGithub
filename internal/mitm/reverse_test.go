package mitm

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"fastgithub-go/internal/config"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testStore(t *testing.T, domains map[string]*config.Domain) *config.Store {
	t.Helper()
	cfg := config.Default()
	cfg.Domains = domains
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func TestReverseProxyDestination(t *testing.T) {
	store := testStore(t, map[string]*config.Domain{
		"fonts.googleapis.com": {Destination: "https://fonts.geekzu.org/"},
	})
	var got *http.Request
	rp := NewReverseProxy(store, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"X-Upstream": {"1"}},
			Body:       io.NopCloser(strings.NewReader("body")),
		}, nil
	}))

	req := httptest.NewRequest(http.MethodGet, "https://fonts.googleapis.com/css?family=Roboto", nil)
	req.Host = "fonts.googleapis.com"
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, req)

	if got == nil {
		t.Fatal("未调用上游")
	}
	if got.URL.Scheme != "https" || got.URL.Host != "fonts.geekzu.org" {
		t.Fatalf("上游 URL = %s", got.URL)
	}
	if got.URL.Path != "/css" || got.URL.RawQuery != "family=Roboto" {
		t.Fatalf("上游路径 = %s?%s", got.URL.Path, got.URL.RawQuery)
	}
	if got.Host != "fonts.googleapis.com" {
		t.Fatalf("Host = %q，应保留原始 Host", got.Host)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "body" {
		t.Fatalf("响应 = %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Upstream") != "1" {
		t.Fatal("响应头未复制")
	}
}

func TestReverseProxyShortCircuit(t *testing.T) {
	store := testStore(t, map[string]*config.Domain{
		"demo.fastgithub.com": {Response: &config.Response{
			Status: http.StatusNotFound, ContentType: "text/plain;charset=utf-8", Content: "nope",
		}},
	})
	called := false
	rp := NewReverseProxy(store, roundTripFunc(func(*http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	}))
	req := httptest.NewRequest(http.MethodGet, "http://demo.fastgithub.com/", nil)
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, req)
	if called {
		t.Fatal("短路响应不应访问上游")
	}
	if rec.Code != http.StatusNotFound || rec.Body.String() != "nope" {
		t.Fatalf("响应 = %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain;charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
}

func TestReverseProxyRemovesHopByHop(t *testing.T) {
	store := testStore(t, map[string]*config.Domain{"example.com": {}})
	var got *http.Request
	rp := NewReverseProxy(store, roundTripFunc(func(r *http.Request) (*http.Response, error) {
		got = r
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}))
	req := httptest.NewRequest(http.MethodGet, "http://example.com/", nil)
	req.Header.Set("Connection", "X-Foo")
	req.Header.Set("X-Foo", "secret")
	req.Header.Set("Proxy-Authorization", "secret")
	rec := httptest.NewRecorder()
	rp.ServeHTTP(rec, req)
	if got.Header.Get("X-Foo") != "" || got.Header.Get("Proxy-Authorization") != "" || got.Header.Get("Connection") != "" {
		t.Fatalf("hop-by-hop 头未移除: %v", got.Header)
	}
}

func TestJoinPath(t *testing.T) {
	cases := []struct{ base, path, want string }{
		{"/", "/css", "/css"},
		{"/base", "/css", "/base/css"},
		{"/base/", "/css", "/base/css"},
		{"/base", "css", "/base/css"},
		{"", "/css", "/css"},
	}
	for _, c := range cases {
		if got := joinPath(c.base, c.path); got != c.want {
			t.Fatalf("joinPath(%q,%q) = %q，期望 %q", c.base, c.path, got, c.want)
		}
	}
}
