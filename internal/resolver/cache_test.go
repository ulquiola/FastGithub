package resolver

import (
	"errors"
	"net"
	"testing"
	"time"
)

func TestIPCacheExpiry(t *testing.T) {
	c := newIPCache(4)
	now := time.Unix(1000, 0)
	c.putPositive("a.example", []net.IP{net.ParseIP("1.2.3.4")}, 30*time.Second, now)

	ips, err, ok := c.get("a.example", now.Add(29*time.Second))
	if !ok || err != nil || len(ips) != 1 {
		t.Fatalf("未过期时 get = %v, %v, %v", ips, err, ok)
	}
	if _, _, ok := c.get("a.example", now.Add(30*time.Second)); ok {
		t.Fatal("到期后应未命中")
	}
}

func TestIPCacheNegative(t *testing.T) {
	c := newIPCache(4)
	now := time.Unix(1000, 0)
	boom := errors.New("boom")
	c.putNegative("a.example", boom, 30*time.Second, now)

	ips, err, ok := c.get("a.example", now.Add(time.Second))
	if !ok || !errors.Is(err, boom) || ips != nil {
		t.Fatalf("负缓存 get = %v, %v, %v", ips, err, ok)
	}
}

func TestIPCacheBounded(t *testing.T) {
	c := newIPCache(4)
	now := time.Unix(1000, 0)
	for i := 0; i < 5; i++ {
		c.putPositive(string(rune('a'+i)), []net.IP{net.ParseIP("1.1.1.1")}, time.Minute, now)
	}
	if len(c.entries) != 1 {
		t.Fatalf("条目数 = %d，期望满时重置后为 1", len(c.entries))
	}
}

func TestIPCacheCopies(t *testing.T) {
	c := newIPCache(4)
	now := time.Unix(1000, 0)
	original := []net.IP{net.ParseIP("1.2.3.4").To4()}
	c.putPositive("a.example", original, time.Minute, now)

	original[0][0] = 9
	ips, _, _ := c.get("a.example", now)
	if !ips[0].Equal(net.ParseIP("1.2.3.4")) {
		t.Fatalf("put 应保存副本，got %v", ips[0])
	}
	ips[0][0] = 8
	again, _, _ := c.get("a.example", now)
	if !again[0].Equal(net.ParseIP("1.2.3.4")) {
		t.Fatalf("get 应返回副本，got %v", again[0])
	}
}

func TestLatencyCacheTTL(t *testing.T) {
	c := newLatencyCache(4)
	now := time.Unix(1000, 0)
	c.put("1.1.1.1", 10*time.Millisecond, true, now)
	c.put("2.2.2.2", 0, false, now)

	if _, ok, found := c.get("1.1.1.1", now.Add(4*time.Minute+59*time.Second)); !found || !ok {
		t.Fatal("成功时延应保留 5min")
	}
	if _, _, found := c.get("1.1.1.1", now.Add(5*time.Minute)); found {
		t.Fatal("成功时延 5min 后应过期")
	}
	if _, ok, found := c.get("2.2.2.2", now.Add(59*time.Second)); !found || ok {
		t.Fatal("失败记录应保留 1min")
	}
	if _, _, found := c.get("2.2.2.2", now.Add(time.Minute)); found {
		t.Fatal("失败记录 1min 后应过期")
	}
}
