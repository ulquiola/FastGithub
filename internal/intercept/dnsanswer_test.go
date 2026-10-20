package intercept

import (
	"encoding/binary"
	"testing"

	"fastgithub-go/internal/config"
)

func testStore(t *testing.T, patterns ...string) *config.Store {
	t.Helper()
	cfg := config.Default()
	cfg.Domains = map[string]*config.Domain{}
	for _, p := range patterns {
		cfg.Domains[p] = &config.Domain{}
	}
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// buildDNSQuery 构造 QDCOUNT=1 的查询。
func buildDNSQuery(name string, qtype uint16, id uint16) []byte {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], id)
	binary.BigEndian.PutUint16(msg[2:4], 0x0100) // RD
	binary.BigEndian.PutUint16(msg[4:6], 1)
	off := 12
	for _, label := range splitLabels(name) {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
		off += 1 + len(label)
	}
	msg = append(msg, 0)
	msg = binary.BigEndian.AppendUint16(msg, qtype)
	msg = binary.BigEndian.AppendUint16(msg, 1)
	return msg
}

func splitLabels(name string) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i <= len(name); i++ {
		if i == len(name) || name[i] == '.' {
			if i > start {
				out = append(out, []byte(name[start:i]))
			}
			start = i + 1
		}
	}
	return out
}

func TestDNSResponseA(t *testing.T) {
	store := testStore(t, "github.com", "*.githubusercontent.com")
	query := buildDNSQuery("github.com", 1, 0xBEEF)
	resp, ok, err := DNSResponse(query, store)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if binary.BigEndian.Uint16(resp[0:2]) != 0xBEEF {
		t.Fatal("ID 未复制")
	}
	flags := binary.BigEndian.Uint16(resp[2:4])
	if flags&0x8000 == 0 || flags&0x0400 == 0 || flags&0x0080 == 0 || flags&0x0100 == 0 {
		t.Fatalf("flags = %#x，期望 QR|AA|RA|RD", flags)
	}
	if binary.BigEndian.Uint16(resp[6:8]) != 1 {
		t.Fatal("ANCOUNT 应为 1")
	}
	if !isPtr(resp[28:30], 0x0C) {
		t.Fatalf("答案名应为压缩指针: % x", resp[28:30])
	}
	if binary.BigEndian.Uint16(resp[30:32]) != 1 || binary.BigEndian.Uint32(resp[34:38]) != dnsTTL {
		t.Fatalf("答案类型/TTL 异常")
	}
	if binary.BigEndian.Uint16(resp[38:40]) != 4 || string(resp[40:44]) != "\x7f\x00\x00\x01" {
		t.Fatalf("A 记录 = % x", resp[40:44])
	}
}

func TestDNSResponseAAAA(t *testing.T) {
	store := testStore(t, "github.com")
	resp, ok, err := DNSResponse(buildDNSQuery("github.com", 28, 1), store)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if binary.BigEndian.Uint16(resp[30:32]) != 28 {
		t.Fatal("QTYPE 应为 AAAA")
	}
	if binary.BigEndian.Uint16(resp[38:40]) != 16 {
		t.Fatal("AAAA rdlength 应为 16")
	}
	want := make([]byte, 16)
	want[15] = 1
	if string(resp[40:56]) != string(want) {
		t.Fatalf("AAAA 地址 = % x", resp[40:56])
	}
}

func TestDNSResponseWildcardMatch(t *testing.T) {
	store := testStore(t, "*.githubusercontent.com")
	if _, ok, _ := DNSResponse(buildDNSQuery("a.githubusercontent.com", 1, 1), store); !ok {
		t.Fatal("通配规则应命中")
	}
}

func TestDNSResponseNotMatched(t *testing.T) {
	store := testStore(t, "github.com")
	_, ok, err := DNSResponse(buildDNSQuery("example.com", 1, 1), store)
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if _, ok, _ := DNSResponse(buildDNSQuery("github.com", 16, 1), store); ok {
		t.Fatal("TXT 查询不应应答")
	}
	if _, ok, _ := DNSResponse(buildDNSQuery("github.com", 1, 1), testStore(t)); ok {
		t.Fatal("空 Store 不应应答")
	}
}

func TestDNSResponseRejectsBadQueries(t *testing.T) {
	store := testStore(t, "github.com")
	if _, _, err := DNSResponse([]byte{1, 2, 3}, store); err == nil {
		t.Fatal("短查询应报错")
	}
	// 压缩指针不支持。
	query := buildDNSQuery("github.com", 1, 1)
	query[12] = 0xC0
	if _, _, err := DNSResponse(query, store); err == nil {
		t.Fatal("压缩指针应报错")
	}
	// 响应包不再应答。
	resp := buildDNSQuery("github.com", 1, 1)
	resp[2] |= 0x80
	if _, ok, _ := DNSResponse(resp, store); ok {
		t.Fatal("响应包不应应答")
	}
}

func isPtr(b []byte, offset int) bool {
	return len(b) == 2 && b[0]&0xC0 == 0xC0 && int(b[0]&0x3F)<<8|int(b[1]) == offset
}
