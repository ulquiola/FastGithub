package dialer

import (
	"net"
	"testing"
	"time"
)

func TestPoolReuseAndExpiry(t *testing.T) {
	p := NewPool(2, time.Minute)
	now := time.Unix(1000, 0)
	p.now = func() time.Time { return now }

	a, peerA := net.Pipe()
	defer peerA.Close()
	p.Put("k", a)
	got, ok := p.Get("k")
	if !ok || got != a {
		t.Fatal("应取回同一连接")
	}
	got.Close()

	b, peerB := net.Pipe()
	defer peerB.Close()
	p.Put("k", b)
	now = now.Add(2 * time.Minute)
	if _, ok := p.Get("k"); ok {
		t.Fatal("过期连接不应返回")
	}
}

func TestPoolMaxAndCloseIdle(t *testing.T) {
	p := NewPool(2, time.Minute)
	var peers []net.Conn
	for i := 0; i < 3; i++ {
		c, peer := net.Pipe()
		peers = append(peers, peer)
		p.Put("k", c)
	}
	if got := len(p.entries["k"]); got != 2 {
		t.Fatalf("空闲连接数 = %d，期望 2", got)
	}
	p.CloseIdle()
	if len(p.entries) != 0 {
		t.Fatalf("CloseIdle 后仍有条目: %v", p.entries)
	}
	for _, peer := range peers {
		peer.Close()
	}
}
