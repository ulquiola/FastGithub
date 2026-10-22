package config

import (
	"fmt"
	"testing"
)

func TestStoreLookupPrefersExact(t *testing.T) {
	c := Default()
	c.Domains = map[string]*Domain{
		"*.github.com": {TLSIgnoreNameMismatch: true},
		"github.com":   {TLSSNI: true},
	}
	st, err := NewStore(c)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	d, ok := st.Lookup("github.com")
	if !ok || !d.TLSSNI {
		t.Fatalf("github.com 应命中精确规则，got=%+v ok=%v", d, ok)
	}
	d, ok = st.Lookup("api.github.com")
	if !ok || !d.TLSIgnoreNameMismatch {
		t.Fatalf("api.github.com 应命中通配规则，got=%+v ok=%v", d, ok)
	}
	if _, ok := st.Lookup("example.com"); ok {
		t.Fatal("example.com 不应命中")
	}
	if got := st.Patterns(); len(got) != 2 {
		t.Fatalf("Patterns() = %v", got)
	}
}

func TestStorePrefersSortedOrder(t *testing.T) {
	c := Default()
	c.Domains = map[string]*Domain{
		"api.*.com":    {TLSIgnoreNameMismatch: true},
		"*.github.com": {TLSSNI: true},
	}
	st, err := NewStore(c)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	d, ok := st.Lookup("api.github.com")
	if !ok || !d.TLSSNI {
		t.Fatalf("api.github.com 应按排序命中 *.github.com，got=%+v ok=%v", d, ok)
	}
	if got := st.Patterns(); len(got) != 2 || got[0] != "*.github.com" {
		t.Fatalf("Patterns() = %v，期望按 Compare 升序", got)
	}
}

func TestStoreCacheKeepsMisses(t *testing.T) {
	c := Default()
	hit := &Domain{TLSSNI: true}
	c.Domains = map[string]*Domain{"github.com": hit}
	st, err := NewStore(c)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if got := st.Config(); got != c {
		t.Fatal("Config() 应返回底层配置")
	}
	if d, ok := st.Lookup("github.com"); !ok || d != hit {
		t.Fatalf("Lookup hit = %+v, %v", d, ok)
	}
	if d, ok := st.Lookup("nope.example"); ok || d != nil {
		t.Fatalf("Lookup miss = %+v, %v", d, ok)
	}
	if v, ok := st.cache["github.com"]; !ok || v != hit {
		t.Fatalf("cache[github.com] = %+v, %v", v, ok)
	}
	if v, ok := st.cache["nope.example"]; !ok || v != nil {
		t.Fatalf("cache[nope.example] = %+v, %v，未命中应缓存 nil", v, ok)
	}
}

func TestStoreCacheBounded(t *testing.T) {
	c := Default()
	c.Domains = map[string]*Domain{"github.com": {}}
	st, err := NewStore(c)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for i := 0; i < maxCacheEntries+4; i++ {
		st.Lookup(fmt.Sprintf("host-%d.example", i))
	}
	if n := len(st.cache); n != 4 {
		t.Fatalf("cache 条目数 = %d，期望上限重置后为 4", n)
	}
	if _, ok := st.Lookup("after-reset.example"); ok {
		t.Fatal("after-reset.example 不应命中")
	}
	if v, ok := st.cache["after-reset.example"]; !ok || v != nil {
		t.Fatalf("cache[after-reset.example] = %+v, %v，期望重置后仍缓存未命中", v, ok)
	}
}

func TestStoreCachesNegative(t *testing.T) {
	c := Default()
	c.Domains = map[string]*Domain{"github.com": {}}
	st, err := NewStore(c)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, ok := st.Lookup("nope.example"); ok {
			t.Fatal("不应命中")
		}
	}
}

func TestStoreRejectsInvalidConfig(t *testing.T) {
	c := Default()
	c.Server.ProxyPort = 0
	if _, err := NewStore(c); err == nil {
		t.Fatal("非法配置应被拒绝")
	}
}

func TestStoreStats(t *testing.T) {
	c := Default()
	c.Domains = map[string]*Domain{
		"*.github.com": {TLSIgnoreNameMismatch: true},
		"github.com":   {TLSSNI: true},
	}
	st, err := NewStore(c)
	if err != nil {
		t.Fatal(err)
	}
	st.Lookup("github.com")
	st.Lookup("api.github.com")
	st.Lookup("api.github.com")
	st.Lookup("example.com")
	stats := st.Stats()
	got := map[string]int64{}
	for _, s := range stats {
		got[s.Pattern] = s.Hits
	}
	if got["github.com"] != 1 || got["*.github.com"] != 2 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestStoreFallbackIPs(t *testing.T) {
	c := Default()
	c.FallbackIPs = map[string][]string{
		"GitHub.com.": {"140.82.113.3", "140.82.114.3"},
	}
	st, err := NewStore(c)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	ips := st.FallbackIPs("github.com")
	if len(ips) != 2 || ips[0].String() != "140.82.113.3" || ips[1].String() != "140.82.114.3" {
		t.Fatalf("FallbackIPs = %v", ips)
	}
	if ips := st.FallbackIPs("api.github.com"); ips != nil {
		t.Fatalf("未配置的域名应返回 nil，got %v", ips)
	}
}
