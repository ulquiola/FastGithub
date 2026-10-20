package resolver

import (
	"net"
	"sync"
	"time"
)

const (
	latencyTTL        = 5 * time.Minute
	failureLatencyTTL = time.Minute
	dohCooldown       = 30 * time.Second
)

type ipCacheEntry struct {
	ips     []net.IP
	err     error
	ttl     time.Duration
	expires time.Time
}

// ipCache 缓存域名解析结果（含负缓存）；满时整体重置，避免无界增长。
type ipCache struct {
	mu      sync.Mutex
	max     int
	entries map[string]ipCacheEntry
}

func newIPCache(max int) *ipCache {
	return &ipCache{max: max, entries: map[string]ipCacheEntry{}}
}

func (c *ipCache) get(domain string, now time.Time) ([]net.IP, error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[domain]
	if !ok {
		return nil, nil, false
	}
	if !now.Before(e.expires) {
		delete(c.entries, domain)
		return nil, nil, false
	}
	return copyIPs(e.ips), e.err, true
}

func (c *ipCache) putPositive(domain string, ips []net.IP, ttl time.Duration, now time.Time) {
	c.put(domain, ipCacheEntry{ips: copyIPs(ips), ttl: ttl}, now)
}

func (c *ipCache) putNegative(domain string, err error, ttl time.Duration, now time.Time) {
	c.put(domain, ipCacheEntry{err: err, ttl: ttl}, now)
}

func (c *ipCache) put(domain string, e ipCacheEntry, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max {
		c.entries = map[string]ipCacheEntry{}
	}
	e.expires = now.Add(e.ttl)
	c.entries[domain] = e
}

type latencyEntry struct {
	latency time.Duration
	ok      bool
	expires time.Time
}

// latencyCache 缓存 IP 拨号时延：成功 5min，失败 1min。
type latencyCache struct {
	mu      sync.Mutex
	max     int
	entries map[string]latencyEntry
}

func newLatencyCache(max int) *latencyCache {
	return &latencyCache{max: max, entries: map[string]latencyEntry{}}
}

func (c *latencyCache) get(ip string, now time.Time) (time.Duration, bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[ip]
	if !ok {
		return 0, false, false
	}
	if !now.Before(e.expires) {
		delete(c.entries, ip)
		return 0, false, false
	}
	return e.latency, e.ok, true
}

func (c *latencyCache) put(ip string, latency time.Duration, ok bool, now time.Time) {
	ttl := latencyTTL
	if !ok {
		ttl = failureLatencyTTL
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max {
		c.entries = map[string]latencyEntry{}
	}
	c.entries[ip] = latencyEntry{latency: latency, ok: ok, expires: now.Add(ttl)}
}

// sourceHealth 记录失败过的 DoH 端点，冷却期内优先尝试其他端点。
type sourceHealth struct {
	mu       sync.Mutex
	cooldown time.Duration
	down     map[string]time.Time
}

func newSourceHealth(cooldown time.Duration) *sourceHealth {
	return &sourceHealth{cooldown: cooldown, down: map[string]time.Time{}}
}

func (h *sourceHealth) available(key string, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	until, ok := h.down[key]
	if !ok {
		return true
	}
	if !now.Before(until) {
		delete(h.down, key)
		return true
	}
	return false
}

func (h *sourceHealth) fail(key string, now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.down[key] = now.Add(h.cooldown)
}

func (h *sourceHealth) ok(key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.down, key)
}

func copyIPs(ips []net.IP) []net.IP {
	if ips == nil {
		return nil
	}
	out := make([]net.IP, len(ips))
	for i, ip := range ips {
		out[i] = append(net.IP(nil), ip...)
	}
	return out
}
