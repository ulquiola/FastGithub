package ui

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/flow"
	"fastgithub-go/internal/resolver"
	"fastgithub-go/internal/sysproxy"
)

func localRequest(method, path string, body ...io.Reader) *http.Request {
	var reader io.Reader
	if len(body) > 0 {
		reader = body[0]
	}
	req := httptest.NewRequest(method, path, reader)
	req.Host = "127.0.0.1"
	return req
}

func newTestUI(t *testing.T) (*Server, *certs.Authority, *int) {
	installs := 0
	s, ca := newTestUIFull(t, &installs, new(int))
	return s, ca, &installs
}

func newTestUIFull(t *testing.T, installs, uninstalls *int) (*Server, *certs.Authority) {
	t.Helper()
	cfg := config.Default()
	cfg.Domains = map[string]*config.Domain{"github.com": {}}
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	store.Lookup("github.com")
	ca, err := certs.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tracker := flow.NewTracker(60)
	tracker.Add(10, 20)
	tracker.Tick()
	ring := flow.NewRing(10)
	ring.Add(flow.Entry{Time: time.Unix(1000, 0), Domain: "github.com", Method: "GET", Status: 200, Read: 1, Write: 2})
	s := New(Options{
		Config: cfg, Store: store, Tracker: tracker, Ring: ring, CA: ca,
		Token: "testtoken", Version: "test",
		InstallCert:   func(string) error { *installs++; return nil },
		UninstallCert: func(string) error { *uninstalls++; return nil },
		CertStatus: func(string) (certs.TrustStatus, error) {
			return certs.TrustStatus{Installed: true, Scope: "user"}, nil
		},
	})
	return s, ca
}

func TestIndexInjectsToken(t *testing.T) {
	s, _, _ := newTestUI(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "testtoken") {
		t.Fatal("页面未注入令牌")
	}
}

func TestStatusAndDomains(t *testing.T) {
	s, _, _ := newTestUI(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/status", nil))
	var status map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status["version"] != "test" || status["proxyPort"].(float64) != 38457 {
		t.Fatalf("status = %v", status)
	}
	if status["caFingerprint"] == "" {
		t.Fatal("缺少 CA 指纹")
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/domains", nil))
	var domains []domainJSON
	if err := json.Unmarshal(rec.Body.Bytes(), &domains); err != nil {
		t.Fatal(err)
	}
	if len(domains) != 1 || domains[0].Pattern != "github.com" || domains[0].Hits != 1 {
		t.Fatalf("domains = %+v", domains)
	}
}

func TestFlowStatistics(t *testing.T) {
	s, _, _ := newTestUI(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/flowStatistics", nil))
	var stats map[string]int64
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats["TotalRead"] != 10 || stats["TotalWrite"] != 20 {
		t.Fatalf("stats = %v", stats)
	}
}

func TestInstallCertRequiresToken(t *testing.T) {
	s, _, installs := newTestUI(t)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodPost, "/api/cert/install", nil))
	if rec.Code != http.StatusUnauthorized || *installs != 0 {
		t.Fatalf("status=%d installs=%d", rec.Code, *installs)
	}

	req := localRequest(http.MethodPost, "/api/cert/install", nil)
	req.Header.Set("X-FastGithub-Token", "testtoken")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || *installs != 1 {
		t.Fatalf("status=%d installs=%d body=%s", rec.Code, *installs, rec.Body.String())
	}
}

func TestInterceptNotImplemented(t *testing.T) {
	s, _, _ := newTestUI(t)
	req := localRequest(http.MethodPost, "/api/intercept", nil)
	req.Header.Set("X-FastGithub-Token", "testtoken")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestCertDownload(t *testing.T) {
	s, _, _ := newTestUI(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/cert", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "BEGIN CERTIFICATE") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "fastgithub.crt") {
		t.Fatal("缺少下载文件名")
	}
}

func TestSSEBacklog(t *testing.T) {
	s, _, _ := newTestUI(t)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/logs", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q", ct)
	}
	reader := bufio.NewReader(resp.Body)
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "data: ") {
		t.Fatalf("line = %q", line)
	}
	var event logEvent
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(line), "data: ")), &event); err != nil {
		t.Fatal(err)
	}
	if event.Domain != "github.com" || event.Method != "GET" {
		t.Fatalf("event = %+v", event)
	}
}

func TestStaticAsset(t *testing.T) {
	s, _, _ := newTestUI(t)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/app.js", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "EventSource") {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestLoadOrCreateToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ui.token")
	token, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(token) != 32 {
		t.Fatalf("token = %q", token)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("权限 = %v", info.Mode().Perm())
		}
	}
	again, err := LoadOrCreateToken(path)
	if err != nil || again != token {
		t.Fatalf("again=%q err=%v", again, err)
	}
}

func TestSSELive(t *testing.T) {
	cfg := config.Default()
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	store.Lookup("github.com")
	ca, err := certs.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ring := flow.NewRing(10)
	s := New(Options{Config: cfg, Store: store, Tracker: flow.NewTracker(1), Ring: ring, CA: ca, Token: "testtoken"})
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/logs", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	ring.Add(flow.Entry{Time: time.Unix(1000, 0), Domain: "live.example", Method: "GET", Status: 200})
	line, err := reader.ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	var event logEvent
	if err := json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(line), "data: ")), &event); err != nil {
		t.Fatal(err)
	}
	if event.Domain != "live.example" {
		t.Fatalf("event = %+v", event)
	}
}

func TestInstallCertRejectsWrongToken(t *testing.T) {
	s, _, installs := newTestUI(t)
	req := localRequest(http.MethodPost, "/api/cert/install")
	req.Header.Set("X-FastGithub-Token", "wrong")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized || *installs != 0 {
		t.Fatalf("status=%d installs=%d", rec.Code, *installs)
	}
}

func TestLoadOrCreateTokenRejectsInvalid(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ui.token")
	if err := os.WriteFile(path, []byte("not-a-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateToken(path); err == nil {
		t.Fatal("非法令牌应报错")
	}
}

func TestUninstallCertRequiresToken(t *testing.T) {
	var installs, uninstalls int
	s, _ := newTestUIFull(t, &installs, &uninstalls)

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodPost, "/api/cert/uninstall"))
	if rec.Code != http.StatusUnauthorized || uninstalls != 0 {
		t.Fatalf("status=%d uninstalls=%d", rec.Code, uninstalls)
	}

	req := localRequest(http.MethodPost, "/api/cert/uninstall")
	req.Header.Set("X-FastGithub-Token", "testtoken")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || uninstalls != 1 {
		t.Fatalf("status=%d uninstalls=%d body=%s", rec.Code, uninstalls, rec.Body.String())
	}
}

func TestUninstallCertRejectsGet(t *testing.T) {
	var installs, uninstalls int
	s, _ := newTestUIFull(t, &installs, &uninstalls)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/cert/uninstall"))
	if rec.Code != http.StatusMethodNotAllowed || uninstalls != 0 {
		t.Fatalf("status=%d uninstalls=%d", rec.Code, uninstalls)
	}
}

func TestCertStatusEndpoint(t *testing.T) {
	var installs, uninstalls int
	s, _ := newTestUIFull(t, &installs, &uninstalls)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/cert/status", nil))
	var body struct {
		Installed bool   `json:"installed"`
		Scope     string `json:"scope"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || !body.Installed || body.Scope != "user" {
		t.Fatalf("status=%d body=%+v", rec.Code, body)
	}
}

func TestSystemProxyEndpoints(t *testing.T) {
	cfg := config.Default()
	cfg.Server.ProxyPort = 38457
	enabled := false
	var calls []string
	s := New(Options{
		Config: cfg, Token: "testtoken",
		Tracker: flow.NewTracker(1), Ring: flow.NewRing(1),
		SystemProxyStatus: func(port int) (sysproxy.Status, error) {
			return sysproxy.Status{
				Supported: true, Enabled: enabled, Service: "Wi-Fi",
				Server: "127.0.0.1", Port: port,
			}, nil
		},
		EnableSystemProxy: func(int) error {
			enabled = true
			calls = append(calls, "enable")
			return nil
		},
		DisableSystemProxy: func(int) error {
			enabled = false
			calls = append(calls, "disable")
			return nil
		},
	})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/systemproxy"))
	var status sysproxy.Status
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if rec.Code != http.StatusOK || status.Enabled || status.Service != "Wi-Fi" {
		t.Fatalf("status=%d body=%+v", rec.Code, status)
	}

	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodPost, "/api/systemproxy/enable"))
	if rec.Code != http.StatusUnauthorized || len(calls) != 0 {
		t.Fatalf("status=%d calls=%v", rec.Code, calls)
	}

	req := localRequest(http.MethodPost, "/api/systemproxy/enable")
	req.Header.Set("X-FastGithub-Token", "testtoken")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !enabled || len(calls) != 1 {
		t.Fatalf("status=%d enabled=%v calls=%v body=%s", rec.Code, enabled, calls, rec.Body.String())
	}

	req = localRequest(http.MethodPost, "/api/systemproxy/disable")
	req.Header.Set("X-FastGithub-Token", "testtoken")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || enabled || len(calls) != 2 {
		t.Fatalf("status=%d enabled=%v calls=%v body=%s", rec.Code, enabled, calls, rec.Body.String())
	}
}

func TestDiagnoseEndpoint(t *testing.T) {
	var gotHost string
	s := New(Options{
		Config: config.Default(), Token: "testtoken",
		Tracker: flow.NewTracker(1), Ring: flow.NewRing(1),
		Diagnose: func(_ context.Context, host string) (resolver.Diagnosis, error) {
			gotHost = host
			return resolver.Diagnosis{
				Host:     host,
				Duration: 1500 * time.Millisecond,
				Candidates: []resolver.Candidate{
					{IP: net.ParseIP("140.82.112.3"), Latency: 42 * time.Millisecond, OK: true, Probed: true},
					{IP: net.ParseIP("20.205.243.166"), OK: false, Probed: true},
				},
			}, nil
		},
	})

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/diagnose?host=github.com"))
	if rec.Code != http.StatusOK || gotHost != "github.com" {
		t.Fatalf("status=%d host=%q body=%s", rec.Code, gotHost, rec.Body.String())
	}
	var body diagnoseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Host != "github.com" || body.DurationMS != 1500 || len(body.Candidates) != 2 {
		t.Fatalf("body = %+v", body)
	}
	if !body.Candidates[0].OK || body.Candidates[0].LatencyMS != 42 {
		t.Fatalf("candidate = %+v", body.Candidates[0])
	}
}

func TestDiagnoseRejectsInvalidHost(t *testing.T) {
	called := false
	s := New(Options{
		Config: config.Default(), Token: "testtoken",
		Tracker: flow.NewTracker(1), Ring: flow.NewRing(1),
		Diagnose: func(context.Context, string) (resolver.Diagnosis, error) {
			called = true
			return resolver.Diagnosis{}, nil
		},
	})
	for _, host := range []string{"", "http://github.com", "github.com:443", "a b", "github.com/path"} {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/diagnose?host="+url.QueryEscape(host)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("host=%q status=%d", host, rec.Code)
		}
	}
	if called {
		t.Fatal("非法 host 不应调用诊断")
	}
}

func TestDiagnoseReportsError(t *testing.T) {
	s := New(Options{
		Config: config.Default(), Token: "testtoken",
		Tracker: flow.NewTracker(1), Ring: flow.NewRing(1),
		Diagnose: func(context.Context, string) (resolver.Diagnosis, error) {
			return resolver.Diagnosis{}, errors.New("dns 失败")
		},
	})
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/diagnose?host=example.com"))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "dns 失败") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
