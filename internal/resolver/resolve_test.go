package resolver

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"fastgithub-go/internal/config"
)

func stubRacer(r *Resolver) {
	r.racer = &racer{
		timeout: 100 * time.Millisecond,
		latency: r.latency,
		dial:    func(context.Context, string, string) (net.Conn, error) { return nopConn{}, nil },
		now:     func() time.Time { return r.now() },
	}
}

func newTestResolver(doh, fallback []string) *Resolver {
	r := New(config.DNS{
		DoH:      doh,
		Fallback: fallback,
		Timeout:  config.Duration(2 * time.Second),
	})
	stubRacer(r)
	return r
}

// startDoH 启动假 DoH 服务并返回请求计数。
func startDoH(t *testing.T, respond func(q dnsQuestion) ([]string, uint32, int)) (*httptest.Server, *int32) {
	t.Helper()
	var count int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&count, 1)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		m, err := parseMessage(body)
		if err != nil || len(m.Questions) == 0 {
			http.Error(w, "parse", http.StatusBadRequest)
			return
		}
		ips, ttl, rcode := respond(m.Questions[0])
		resp := responseForQuery(body, ips, ttl, rcode)
		if resp == nil {
			http.Error(w, "build", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(resp)
	}))
	t.Cleanup(srv.Close)
	return srv, &count
}

func TestResolverLookupDoH(t *testing.T) {
	srv, _ := startDoH(t, func(q dnsQuestion) ([]string, uint32, int) {
		if q.Type == typeA {
			return []string{"1.2.3.4", "5.6.7.8"}, 60, 0
		}
		return []string{"2001:db8::1"}, 60, 0
	})
	r := newTestResolver([]string{srv.URL}, nil)
	ips, err := r.Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(ips) != 3 {
		t.Fatalf("ips = %v", ips)
	}
}

func TestResolverFallsBackToTCP(t *testing.T) {
	dohSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer dohSrv.Close()
	tcpAddr := startTCPDNS(t, func(query []byte) []byte {
		m, err := parseMessage(query)
		if err != nil || len(m.Questions) == 0 {
			return nil
		}
		if m.Questions[0].Type == typeA {
			return responseForQuery(query, []string{"1.2.3.4"}, 60, 0)
		}
		return responseForQuery(query, []string{"2001:db8::1"}, 60, 0)
	})
	r := newTestResolver([]string{dohSrv.URL}, []string{tcpAddr})
	ips, err := r.Lookup(context.Background(), "example.com")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if len(ips) != 2 {
		t.Fatalf("ips = %v", ips)
	}
}

func TestResolverTTLClamp(t *testing.T) {
	srv, count := startDoH(t, func(dnsQuestion) ([]string, uint32, int) {
		return []string{"1.2.3.4"}, 1, 0
	})
	r := newTestResolver([]string{srv.URL}, nil)
	now := time.Unix(1000, 0)
	r.now = func() time.Time { return now }

	if _, err := r.Lookup(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(count); got != 2 {
		t.Fatalf("首次请求数 = %d，期望 2（A+AAAA）", got)
	}
	now = now.Add(29 * time.Second)
	if _, err := r.Lookup(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(count); got != 2 {
		t.Fatalf("29s 内应命中缓存，请求数 = %d", got)
	}
	now = now.Add(2 * time.Second)
	if _, err := r.Lookup(context.Background(), "example.com"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(count); got != 4 {
		t.Fatalf("31s 后应重新查询，请求数 = %d", got)
	}
}

func TestResolverNegativeCache(t *testing.T) {
	srv, count := startDoH(t, func(dnsQuestion) ([]string, uint32, int) {
		return nil, 0, 3
	})
	r := newTestResolver([]string{srv.URL}, nil)
	now := time.Unix(1000, 0)
	r.now = func() time.Time { return now }

	_, err := r.Lookup(context.Background(), "nope.example")
	if !errors.Is(err, errNXDOMAIN) {
		t.Fatalf("err = %v", err)
	}
	if got := atomic.LoadInt32(count); got != 2 {
		t.Fatalf("首次请求数 = %d", got)
	}
	now = now.Add(29 * time.Second)
	if _, err := r.Lookup(context.Background(), "nope.example"); !errors.Is(err, errNXDOMAIN) {
		t.Fatalf("负缓存命中 err = %v", err)
	}
	if got := atomic.LoadInt32(count); got != 2 {
		t.Fatalf("29s 内应命中负缓存，请求数 = %d", got)
	}
	now = now.Add(2 * time.Second)
	if _, err := r.Lookup(context.Background(), "nope.example"); !errors.Is(err, errNXDOMAIN) {
		t.Fatalf("err = %v", err)
	}
	if got := atomic.LoadInt32(count); got != 4 {
		t.Fatalf("31s 后应重新查询，请求数 = %d", got)
	}
}

func TestResolverPartialSuccess(t *testing.T) {
	srv, _ := startDoH(t, func(q dnsQuestion) ([]string, uint32, int) {
		if q.Type == typeA {
			return []string{"1.2.3.4"}, 60, 0
		}
		return nil, 0, 2 // SERVFAIL
	})
	r := newTestResolver([]string{srv.URL}, nil)
	ips, err := r.Lookup(context.Background(), "example.com")
	if err != nil || len(ips) != 1 {
		t.Fatalf("ips=%v err=%v", ips, err)
	}
}

func TestResolverSingleflight(t *testing.T) {
	srv, count := startDoH(t, func(dnsQuestion) ([]string, uint32, int) {
		time.Sleep(100 * time.Millisecond)
		return []string{"1.2.3.4"}, 60, 0
	})
	r := newTestResolver([]string{srv.URL}, nil)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := r.Lookup(context.Background(), "example.com"); err != nil {
				t.Errorf("Lookup: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := atomic.LoadInt32(count); got != 2 {
		t.Fatalf("请求数 = %d，期望 2（A+AAAA 各一次）", got)
	}
}

func TestResolverNormalizesDomain(t *testing.T) {
	srv, count := startDoH(t, func(dnsQuestion) ([]string, uint32, int) {
		return []string{"1.2.3.4"}, 60, 0
	})
	r := newTestResolver([]string{srv.URL}, nil)
	if _, err := r.Lookup(context.Background(), "GitHub.COM."); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Lookup(context.Background(), "github.com"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(count); got != 2 {
		t.Fatalf("请求数 = %d，规范化后应命中缓存", got)
	}
}

func TestResolverNoSources(t *testing.T) {
	r := newTestResolver(nil, nil)
	_, err := r.Lookup(context.Background(), "example.com")
	if err == nil || !strings.Contains(err.Error(), "没有可用的 DNS 源") {
		t.Fatalf("err = %v", err)
	}
}

func TestResolverEmptyDomain(t *testing.T) {
	r := newTestResolver(nil, nil)
	if _, err := r.Lookup(context.Background(), "."); err == nil {
		t.Fatal("空域名应报错")
	}
}

func TestNegativeTTL(t *testing.T) {
	if got := negativeTTL(errors.New("bad rcode")); got != nonSocketNegativeTTL {
		t.Fatalf("非 socket 错误 TTL = %v", got)
	}
	opErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("refused")}
	if got := negativeTTL(opErr); got != socketNegativeTTL {
		t.Fatalf("socket 错误 TTL = %v", got)
	}
}

func TestResolverCallerTimeoutDoesNotPoisonCache(t *testing.T) {
	srv, count := startDoH(t, func(dnsQuestion) ([]string, uint32, int) {
		time.Sleep(200 * time.Millisecond)
		return []string{"1.2.3.4"}, 60, 0
	})
	r := newTestResolver([]string{srv.URL}, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := r.Lookup(ctx, "example.com"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v，期望调用方超时", err)
	}
	if elapsed := time.Since(start); elapsed > 150*time.Millisecond {
		t.Fatalf("调用方超时未及时返回: %v", elapsed)
	}

	// 共享查询继续完成并写入正缓存，不应留下 10 分钟负缓存。
	time.Sleep(300 * time.Millisecond)
	ips, err := r.Lookup(context.Background(), "example.com")
	if err != nil || len(ips) != 1 {
		t.Fatalf("ips=%v err=%v", ips, err)
	}
	if got := atomic.LoadInt32(count); got != 2 {
		t.Fatalf("请求数 = %d，期望 2", got)
	}
}

func TestResolverDoHHealthCooldown(t *testing.T) {
	bad, badCount := startDoH(t, func(dnsQuestion) ([]string, uint32, int) {
		return nil, 0, 2 // SERVFAIL
	})
	good, _ := startDoH(t, func(q dnsQuestion) ([]string, uint32, int) {
		if q.Type == typeA {
			return []string{"1.2.3.4"}, 60, 0
		}
		return []string{"2001:db8::1"}, 60, 0
	})
	r := newTestResolver([]string{bad.URL, good.URL}, nil)
	if _, err := r.Lookup(context.Background(), "one.example"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(badCount); got < 1 || got > 2 {
		t.Fatalf("首次应尝试坏端点（A/AAAA 并发），请求数 = %d", got)
	}
	if _, err := r.Lookup(context.Background(), "two.example"); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(badCount); got != 2 {
		t.Fatalf("冷却期内不应再尝试坏端点，请求数 = %d", got)
	}
}

func TestResolverOverallTimeout(t *testing.T) {
	srv, _ := startDoH(t, func(dnsQuestion) ([]string, uint32, int) {
		time.Sleep(time.Second)
		return []string{"1.2.3.4"}, 60, 0
	})
	r := New(config.DNS{
		DoH:     []string{srv.URL, srv.URL, srv.URL, srv.URL},
		Timeout: config.Duration(100 * time.Millisecond),
	})
	stubRacer(r)
	start := time.Now()
	if _, err := r.Lookup(context.Background(), "example.com"); err == nil {
		t.Fatal("应超时")
	}
	if elapsed := time.Since(start); elapsed > 250*time.Millisecond {
		t.Fatalf("耗时 %v，dns.timeout 应作为总预算并按源均分", elapsed)
	}
}

func TestResolverNODATADoesNotCoolDownEndpoint(t *testing.T) {
	var firstCalls int32
	first, _ := startDoH(t, func(q dnsQuestion) ([]string, uint32, int) {
		if atomic.AddInt32(&firstCalls, 1) <= 2 {
			return nil, 0, 0 // 第一个域名的 A/AAAA 都是 NODATA
		}
		if q.Type == typeA {
			return []string{"1.2.3.4"}, 60, 0
		}
		return []string{"2001:db8::1"}, 60, 0
	})
	second, _ := startDoH(t, func(q dnsQuestion) ([]string, uint32, int) {
		if q.Type == typeA {
			return []string{"5.6.7.8"}, 60, 0
		}
		return []string{"2001:db8::2"}, 60, 0
	})
	r := newTestResolver([]string{first.URL, second.URL}, nil)
	if _, err := r.Lookup(context.Background(), "empty.example"); err != nil {
		t.Fatalf("第二个端点应能应答: %v", err)
	}
	ips, err := r.Lookup(context.Background(), "good.example")
	if err != nil || len(ips) != 2 {
		t.Fatalf("NODATA 不应让端点进入冷却: ips=%v err=%v", ips, err)
	}
	if got := atomic.LoadInt32(&firstCalls); got != 4 {
		t.Fatalf("NODATA 后首个端点应仍被优先尝试，请求数 = %d，期望 4", got)
	}
}

func TestResolverFallbackGetsBudgetShare(t *testing.T) {
	slow, _ := startDoH(t, func(dnsQuestion) ([]string, uint32, int) {
		time.Sleep(time.Second)
		return []string{"9.9.9.9"}, 60, 0
	})
	tcpAddr := startTCPDNS(t, func(query []byte) []byte {
		m, err := parseMessage(query)
		if err != nil || len(m.Questions) == 0 {
			return nil
		}
		if m.Questions[0].Type == typeA {
			return responseForQuery(query, []string{"1.2.3.4"}, 60, 0)
		}
		return responseForQuery(query, []string{"2001:db8::1"}, 60, 0)
	})
	r := New(config.DNS{
		DoH:      []string{slow.URL},
		Fallback: []string{tcpAddr},
		Timeout:  config.Duration(200 * time.Millisecond),
	})
	stubRacer(r)
	ips, err := r.Lookup(context.Background(), "example.com")
	if err != nil || len(ips) != 2 {
		t.Fatalf("回退应获得预算份额: ips=%v err=%v", ips, err)
	}
}
