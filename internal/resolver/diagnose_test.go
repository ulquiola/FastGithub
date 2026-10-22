package resolver

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"fastgithub-go/internal/config"
)

type fakeConn struct{}

func (fakeConn) Read([]byte) (int, error)         { return 0, errors.New("not implemented") }
func (fakeConn) Write(p []byte) (int, error)      { return len(p), nil }
func (fakeConn) Close() error                     { return nil }
func (fakeConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (fakeConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (fakeConn) SetDeadline(time.Time) error      { return nil }
func (fakeConn) SetReadDeadline(time.Time) error  { return nil }
func (fakeConn) SetWriteDeadline(time.Time) error { return nil }

func TestDiagnoseSortsAndMarksFailures(t *testing.T) {
	r := New(config.DNS{})
	good := net.ParseIP("140.82.112.3")
	bad := net.ParseIP("20.205.243.166")
	r.queryFn = func(context.Context, string) ([]net.IP, uint32, error) {
		return []net.IP{bad, good}, 60, nil
	}
	r.racer.timeout = 100 * time.Millisecond
	r.racer.dial = func(_ context.Context, _, addr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(addr)
		if host == good.String() {
			return fakeConn{}, nil
		}
		return nil, errors.New("dial timeout")
	}

	d, err := r.Diagnose(context.Background(), "Example.COM.")
	if err != nil {
		t.Fatal(err)
	}
	if d.Host != "example.com" || len(d.Candidates) != 2 {
		t.Fatalf("diagnosis = %+v", d)
	}
	if !d.Candidates[0].OK || d.Candidates[0].IP.String() != good.String() {
		t.Fatalf("第一个候选 = %+v，期望可用且为 %s", d.Candidates[0], good)
	}
	if d.Candidates[1].OK || !d.Candidates[1].Probed {
		t.Fatalf("第二个候选 = %+v，期望探测失败", d.Candidates[1])
	}
	if _, _, ok := r.cache.get("example.com", r.now()); ok {
		t.Fatal("诊断不应写入解析缓存")
	}
}

func TestDiagnoseQueryError(t *testing.T) {
	r := New(config.DNS{})
	r.queryFn = func(context.Context, string) ([]net.IP, uint32, error) {
		return nil, 0, errors.New("dns boom")
	}
	if _, err := r.Diagnose(context.Background(), "example.com"); err == nil ||
		!strings.Contains(err.Error(), "dns boom") {
		t.Fatalf("err = %v", err)
	}
}

func TestDiagnoseIPLiteralSkipsDNS(t *testing.T) {
	r := New(config.DNS{})
	called := false
	r.queryFn = func(context.Context, string) ([]net.IP, uint32, error) {
		called = true
		return nil, 0, errors.New("should not be called")
	}
	r.racer.dial = func(context.Context, string, string) (net.Conn, error) {
		return fakeConn{}, nil
	}
	d, err := r.Diagnose(context.Background(), "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("IP 字面量不应触发 DNS 查询")
	}
	if len(d.Candidates) != 1 || !d.Candidates[0].OK {
		t.Fatalf("diagnosis = %+v", d)
	}
}
