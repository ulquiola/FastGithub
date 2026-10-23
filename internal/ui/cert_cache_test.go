package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/flow"
)

// certStatusFixture 记录证书状态探测次数，并允许测试直接改状态。
type certStatusFixture struct {
	mu        sync.Mutex
	calls     int
	installed bool
}

func (f *certStatusFixture) status(string) (certs.TrustStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	scope := "none"
	if f.installed {
		scope = "system"
	}
	return certs.TrustStatus{Installed: f.installed, Scope: scope}, nil
}

func (f *certStatusFixture) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *certStatusFixture) set(installed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installed = installed
}

// newCertCacheServer 构造只关心证书状态的最小服务器，并注入可推进的时钟。
func newCertCacheServer(t *testing.T, fx *certStatusFixture, ttl time.Duration, now *time.Time) *Server {
	t.Helper()
	ca, err := certs.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	s := New(Options{
		Config: cfg, CA: ca, Token: "testtoken",
		Tracker: flow.NewTracker(1), Ring: flow.NewRing(1),
		CertStatus:    fx.status,
		CertStatusTTL: ttl,
		InstallCert:   func(string) error { fx.set(true); return nil },
		UninstallCert: func(string) error { fx.set(false); return nil },
	})
	s.now = func() time.Time { return *now }
	return s
}

func getCertStatus(t *testing.T, s *Server) bool {
	t.Helper()
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, localRequest(http.MethodGet, "/api/cert/status", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Installed bool `json:"installed"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Installed
}

func postCertAction(t *testing.T, s *Server, path string) {
	t.Helper()
	req := localRequest(http.MethodPost, path)
	req.Header.Set("X-FastGithub-Token", "testtoken")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s status=%d body=%s", path, rec.Code, rec.Body.String())
	}
}

// TestCertStatusCachedWithinTTL 保证 UI 每 5 秒轮询不会每次都拉起一个 certutil 进程。
func TestCertStatusCachedWithinTTL(t *testing.T) {
	fx := &certStatusFixture{installed: true}
	now := time.Unix(1000, 0)
	s := newCertCacheServer(t, fx, 30*time.Second, &now)

	getCertStatus(t, s)
	getCertStatus(t, s)
	if got := fx.callCount(); got != 1 {
		t.Fatalf("CertStatus 调用 %d 次，期望 1（TTL 内应命中缓存）", got)
	}
}

// TestCertStatusRefreshedAfterTTL 保证缓存会过期，外部改动最终能被看到。
func TestCertStatusRefreshedAfterTTL(t *testing.T) {
	fx := &certStatusFixture{installed: false}
	now := time.Unix(1000, 0)
	s := newCertCacheServer(t, fx, 30*time.Second, &now)

	if getCertStatus(t, s) {
		t.Fatal("初始状态应为未安装")
	}
	fx.set(true)
	now = now.Add(31 * time.Second)
	if !getCertStatus(t, s) {
		t.Fatal("缓存过期后应重新探测并返回已安装")
	}
	if got := fx.callCount(); got != 2 {
		t.Fatalf("CertStatus 调用 %d 次，期望 2（过期后重新探测）", got)
	}
}

// TestCertStatusCacheInvalidatedByInstall 保证点“安装”后界面立刻显示已安装，
// 而不是等缓存过期。
func TestCertStatusCacheInvalidatedByInstall(t *testing.T) {
	fx := &certStatusFixture{installed: false}
	now := time.Unix(1000, 0)
	s := newCertCacheServer(t, fx, 30*time.Second, &now)

	if getCertStatus(t, s) {
		t.Fatal("初始状态应为未安装")
	}
	postCertAction(t, s, "/api/cert/install")
	if !getCertStatus(t, s) {
		t.Fatal("安装后应立刻显示已安装（缓存需失效）")
	}
	if got := fx.callCount(); got != 2 {
		t.Fatalf("CertStatus 调用 %d 次，期望 2（安装后重新探测）", got)
	}
}

// TestCertStatusCacheInvalidatedByUninstall 是安装场景的镜像。
func TestCertStatusCacheInvalidatedByUninstall(t *testing.T) {
	fx := &certStatusFixture{installed: true}
	now := time.Unix(1000, 0)
	s := newCertCacheServer(t, fx, 30*time.Second, &now)

	if !getCertStatus(t, s) {
		t.Fatal("初始状态应为已安装")
	}
	postCertAction(t, s, "/api/cert/uninstall")
	if getCertStatus(t, s) {
		t.Fatal("卸载后应立刻显示未安装（缓存需失效）")
	}
	if got := fx.callCount(); got != 2 {
		t.Fatalf("CertStatus 调用 %d 次，期望 2（卸载后重新探测）", got)
	}
}

// TestCertStatusUsesDefaultTTL 保证 Options 不填 TTL 时也有缓存，避免零值退化成
// “永不缓存”或“永不过期”。
func TestCertStatusUsesDefaultTTL(t *testing.T) {
	fx := &certStatusFixture{installed: true}
	now := time.Unix(1000, 0)
	s := newCertCacheServer(t, fx, 0, &now)

	getCertStatus(t, s)
	getCertStatus(t, s)
	if got := fx.callCount(); got != 1 {
		t.Fatalf("默认 TTL 下调用 %d 次，期望 1", got)
	}
	now = now.Add(defaultCertStatusTTL + time.Second)
	getCertStatus(t, s)
	if got := fx.callCount(); got != 2 {
		t.Fatalf("超过默认 TTL 后调用 %d 次，期望 2", got)
	}
}
