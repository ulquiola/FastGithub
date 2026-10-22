package resolver

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"fastgithub-go/internal/config"
)

// dohTestServer 返回一个只解析 linux.do 的最小 DoH 端点。
func dohTestServer(t *testing.T, ip string, hits *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits != nil {
			hits.Add(1)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) < 12 {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		id := binary.BigEndian.Uint16(body[:2])
		qtype := binary.BigEndian.Uint16(body[len(body)-4:])
		var recs []testRecord
		if qtype == typeA {
			recs = []testRecord{{name: "linux.do", typ: typeA, ttl: 60, ip: net.ParseIP(ip)}}
		}
		resp, err := buildResponse(id, "linux.do", qtype, recs, 0)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(resp)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLookupSourceUsesCustomDoH(t *testing.T) {
	var customHits, defaultHits atomic.Int32
	custom := dohTestServer(t, "10.0.0.1", &customHits)
	def := dohTestServer(t, "10.0.0.2", &defaultHits)
	r := New(config.DNS{DoH: []string{def.URL}})
	r.racer.dial = func(context.Context, string, string) (net.Conn, error) { return fakeConn{}, nil }

	ips, err := r.LookupSource(context.Background(), "linux.do", custom.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 1 || ips[0].String() != "10.0.0.1" {
		t.Fatalf("ips = %v", ips)
	}
	if customHits.Load() == 0 {
		t.Fatal("未使用自定义 DoH")
	}
	if defaultHits.Load() != 0 {
		t.Fatalf("不应访问全局 DoH，实际 %d 次", defaultHits.Load())
	}
}

func TestLookupSourceFallsBackToGlobal(t *testing.T) {
	var defaultHits atomic.Int32
	def := dohTestServer(t, "10.0.0.2", &defaultHits)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(bad.Close)

	r := New(config.DNS{DoH: []string{def.URL}})
	r.racer.dial = func(context.Context, string, string) (net.Conn, error) { return fakeConn{}, nil }

	ips, err := r.LookupSource(context.Background(), "linux.do", bad.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(ips) != 1 || ips[0].String() != "10.0.0.2" {
		t.Fatalf("ips = %v", ips)
	}
	if defaultHits.Load() == 0 {
		t.Fatal("自定义源失败后应回退全局 DoH")
	}
}

func TestDiagnoseSourceUsesCustomDoH(t *testing.T) {
	var customHits, defaultHits atomic.Int32
	custom := dohTestServer(t, "10.0.0.1", &customHits)
	def := dohTestServer(t, "10.0.0.2", &defaultHits)
	r := New(config.DNS{DoH: []string{def.URL}})
	r.racer.dial = func(context.Context, string, string) (net.Conn, error) { return fakeConn{}, nil }

	d, err := r.DiagnoseSource(context.Background(), "linux.do", custom.URL)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Candidates) != 1 || d.Candidates[0].IP.String() != "10.0.0.1" || !d.Candidates[0].OK {
		t.Fatalf("diagnosis = %+v", d)
	}
	if customHits.Load() == 0 || defaultHits.Load() != 0 {
		t.Fatalf("custom=%d default=%d", customHits.Load(), defaultHits.Load())
	}
}
