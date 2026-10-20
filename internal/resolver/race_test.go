package resolver

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

var errShortResult = errors.New("测试错误")

type nopConn struct{}

func (nopConn) Read([]byte) (int, error)         { return 0, io.EOF }
func (nopConn) Write(b []byte) (int, error)      { return len(b), nil }
func (nopConn) Close() error                     { return nil }
func (nopConn) LocalAddr() net.Addr              { return nopAddr{} }
func (nopConn) RemoteAddr() net.Addr             { return nopAddr{} }
func (nopConn) SetDeadline(time.Time) error      { return nil }
func (nopConn) SetReadDeadline(time.Time) error  { return nil }
func (nopConn) SetWriteDeadline(time.Time) error { return nil }

type nopAddr struct{}

func (nopAddr) Network() string { return "tcp" }
func (nopAddr) String() string  { return "" }

func TestRacerSortsByLatency(t *testing.T) {
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(addr)
		switch host {
		case "1.1.1.1":
			time.Sleep(30 * time.Millisecond)
			return nopConn{}, nil
		case "2.2.2.2":
			return nopConn{}, nil
		default:
			return nil, errors.New("refused")
		}
	}
	r := &racer{
		timeout: 2 * time.Second,
		port:    "443",
		latency: newLatencyCache(16),
		dial:    dial,
		now:     time.Now,
	}
	ips := []net.IP{net.ParseIP("3.3.3.3"), net.ParseIP("1.1.1.1"), net.ParseIP("2.2.2.2")}
	got := r.Sort(context.Background(), ips)
	want := []string{"2.2.2.2", "1.1.1.1", "3.3.3.3"}
	for i, ip := range got {
		if ip.String() != want[i] {
			t.Fatalf("顺序 = %v，期望 %v", got, want)
		}
	}
}

func TestRacerAllFailKeepsOrder(t *testing.T) {
	r := &racer{
		timeout: 50 * time.Millisecond,
		port:    "443",
		latency: newLatencyCache(16),
		dial: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("refused")
		},
		now: time.Now,
	}
	ips := []net.IP{net.ParseIP("3.3.3.3"), net.ParseIP("1.1.1.1")}
	got := r.Sort(context.Background(), ips)
	if got[0].String() != "3.3.3.3" || got[1].String() != "1.1.1.1" {
		t.Fatalf("全部失败应保持原序，got %v", got)
	}
}

func TestRacerUsesCachedLatency(t *testing.T) {
	lc := newLatencyCache(16)
	lc.put("1.1.1.1", 0, true, time.Now())
	var calls int32
	r := &racer{
		timeout: time.Second,
		port:    "443",
		latency: lc,
		dial: func(context.Context, string, string) (net.Conn, error) {
			calls++
			time.Sleep(20 * time.Millisecond)
			return nopConn{}, nil
		},
		now: time.Now,
	}
	got := r.Sort(context.Background(), []net.IP{net.ParseIP("2.2.2.2"), net.ParseIP("1.1.1.1")})
	if calls != 1 {
		t.Fatalf("只应探测未缓存的 IP，实际拨号 %d 次", calls)
	}
	if got[0].String() != "1.1.1.1" {
		t.Fatalf("缓存时延应排首位，got %v", got)
	}
}
