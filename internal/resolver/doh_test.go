package resolver

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDoHQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/dns-message" {
			t.Errorf("Content-Type = %q", got)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(makeResponseForQuery(t, body, []string{"1.2.3.4"}, 60, 0))
	}))
	defer srv.Close()

	c := &dohClient{endpoints: []string{srv.URL}, client: srv.Client()}
	msg, id, err := buildQueryWithID("example.com", typeA, 0x1234)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.query(context.Background(), srv.URL, msg)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	ips, ttl, err := parseAnswer(resp, id, "example.com", typeA)
	if err != nil || len(ips) != 1 || ttl != 60 {
		t.Fatalf("ips=%v ttl=%d err=%v", ips, ttl, err)
	}
}

func TestDoHNon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()
	c := &dohClient{endpoints: []string{srv.URL}, client: srv.Client()}
	if _, err := c.query(context.Background(), srv.URL, make([]byte, 12)); err == nil {
		t.Fatal("非 200 响应应报错")
	}
}

func TestQueryTCP(t *testing.T) {
	addr := startTCPDNS(t, func(query []byte) []byte {
		return makeResponseForQuery(t, query, []string{"1.2.3.4"}, 60, 0)
	})
	msg, id, err := buildQueryWithID("example.com", typeA, 0x2222)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := queryTCP(ctx, addr, msg)
	if err != nil {
		t.Fatalf("queryTCP: %v", err)
	}
	ips, _, err := parseAnswer(resp, id, "example.com", typeA)
	if err != nil || len(ips) != 1 {
		t.Fatalf("ips=%v err=%v", ips, err)
	}
}

func TestParseAnswerErrors(t *testing.T) {
	_, id, err := buildQueryWithID("example.com", typeA, 0x1234)
	if err != nil {
		t.Fatal(err)
	}
	good := buildTestResponse(t, id, "example.com", typeA, []testRecord{
		{name: "example.com", typ: typeA, ttl: 60, ip: []byte{1, 2, 3, 4}},
	}, 0)

	if _, _, err := parseAnswer(good, id+1, "example.com", typeA); err == nil || !strings.Contains(err.Error(), "ID") {
		t.Fatalf("ID 不匹配应报错: %v", err)
	}
	noQR := append([]byte(nil), good...)
	noQR[2] &= 0x7F
	if _, _, err := parseAnswer(noQR, id, "example.com", typeA); err == nil || !strings.Contains(err.Error(), "QR") {
		t.Fatalf("缺少 QR 应报错: %v", err)
	}
	nx := buildTestResponse(t, id, "example.com", typeA, nil, 3)
	if _, _, err := parseAnswer(nx, id, "example.com", typeA); !errors.Is(err, errNXDOMAIN) {
		t.Fatalf("NXDOMAIN err = %v", err)
	}
	servfail := buildTestResponse(t, id, "example.com", typeA, nil, 2)
	if _, _, err := parseAnswer(servfail, id, "example.com", typeA); err == nil || !strings.Contains(err.Error(), "错误码 2") {
		t.Fatalf("SERVFAIL err = %v", err)
	}
	empty := buildTestResponse(t, id, "example.com", typeA, nil, 0)
	if _, _, err := parseAnswer(empty, id, "example.com", typeA); err == nil || !strings.Contains(err.Error(), "无 A/AAAA") {
		t.Fatalf("空答案 err = %v", err)
	}
	wrongName := buildTestResponse(t, id, "other.com", typeA, nil, 0)
	if _, _, err := parseAnswer(wrongName, id, "example.com", typeA); err == nil || !strings.Contains(err.Error(), "问题名") {
		t.Fatalf("问题名不匹配 err = %v", err)
	}
}
