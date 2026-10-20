package resolver

import (
	"bytes"
	"net"
	"strings"
	"testing"
)

func TestBuildQueryGolden(t *testing.T) {
	msg, id, err := buildQueryWithID("example.com", typeA, 0x1234)
	if err != nil {
		t.Fatal(err)
	}
	if id != 0x1234 {
		t.Fatalf("id = %#x", id)
	}
	want := []byte{
		0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
		0x00, 0x01, 0x00, 0x01,
	}
	if !bytes.Equal(msg, want) {
		t.Fatalf("query = % x\nwant  = % x", msg, want)
	}
}

func TestParseCompressedCNAME(t *testing.T) {
	resp := buildTestResponse(t, 0x1234, "example.com", typeA, []testRecord{
		{name: "example.com", typ: typeCNAME, ttl: 60, target: "cdn.example.net"},
		{name: "cdn.example.net", typ: typeA, ttl: 30, ip: net.ParseIP("1.2.3.4")},
	}, 0)
	m, err := parseMessage(resp)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Questions) != 1 || m.Questions[0].Name != "example.com" {
		t.Fatalf("questions = %+v", m.Questions)
	}
	ips, ttl := m.addresses("example.com", typeA)
	if len(ips) != 1 || !ips[0].Equal(net.ParseIP("1.2.3.4")) || ttl != 30 {
		t.Fatalf("ips=%v ttl=%d", ips, ttl)
	}
}

func TestParseAAAA(t *testing.T) {
	resp := buildTestResponse(t, 1, "example.com", typeAAAA, []testRecord{
		{name: "example.com", typ: typeAAAA, ttl: 120, ip: net.ParseIP("2001:db8::1")},
	}, 0)
	m, err := parseMessage(resp)
	if err != nil {
		t.Fatal(err)
	}
	ips, ttl := m.addresses("example.com", typeAAAA)
	if len(ips) != 1 || !ips[0].Equal(net.ParseIP("2001:db8::1")) || ttl != 120 {
		t.Fatalf("ips=%v ttl=%d", ips, ttl)
	}
}

func TestParseRejectsPointerLoop(t *testing.T) {
	msg := make([]byte, 14)
	msg[5] = 1 // QDCOUNT
	msg[12], msg[13] = 0xC0, 0x0C
	if _, err := parseMessage(msg); err == nil {
		t.Fatal("压缩指针循环应报错")
	}
}

func TestParseRejectsTruncated(t *testing.T) {
	header := make([]byte, 12)
	header[5] = 1 // QDCOUNT
	truncatedQuestion := append(header, 7, 'e')
	for _, data := range [][]byte{
		{1, 2, 3},
		truncatedQuestion,
	} {
		if _, err := parseMessage(data); err == nil {
			t.Fatalf("parseMessage(% x) 应报错", data)
		}
	}
}

func TestSplitNameErrors(t *testing.T) {
	names := []string{"", "a..b", strings.Repeat("a", 64) + ".com",
		strings.Join([]string{strings.Repeat("a", 63), strings.Repeat("b", 63), strings.Repeat("c", 63), strings.Repeat("d", 63)}, ".")}
	for _, name := range names {
		if _, err := splitName(name); err == nil {
			t.Fatalf("splitName(%q) 应报错", name)
		}
	}
}

func TestAddressesCNAMELoop(t *testing.T) {
	resp := buildTestResponse(t, 1, "a.example", typeA, []testRecord{
		{name: "a.example", typ: typeCNAME, ttl: 60, target: "b.example"},
		{name: "b.example", typ: typeCNAME, ttl: 60, target: "a.example"},
	}, 0)
	m, err := parseMessage(resp)
	if err != nil {
		t.Fatal(err)
	}
	if ips, _ := m.addresses("a.example", typeA); len(ips) != 0 {
		t.Fatalf("CNAME 环不应返回地址: %v", ips)
	}
}
