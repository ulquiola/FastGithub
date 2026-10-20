package resolver

import (
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
)

type testRecord struct {
	name   string
	typ    uint16
	ttl    uint32
	ip     net.IP
	target string
}

// buildResponse 构造响应报文；同名记录使用压缩指针，CNAME 目标也可被后续记录引用。
func buildResponse(id uint16, qname string, qtype uint16, recs []testRecord, rcode int) ([]byte, error) {
	labels, err := splitName(qname)
	if err != nil {
		return nil, err
	}
	buf := make([]byte, 12)
	binary.BigEndian.PutUint16(buf[0:2], id)
	binary.BigEndian.PutUint16(buf[2:4], 0x8180|uint16(rcode))
	binary.BigEndian.PutUint16(buf[4:6], 1)
	binary.BigEndian.PutUint16(buf[6:8], uint16(len(recs)))

	offsets := map[string]int{strings.ToLower(qname): 12}
	for _, label := range labels {
		buf = append(buf, byte(len(label)))
		buf = append(buf, label...)
	}
	buf = append(buf, 0)
	buf = binary.BigEndian.AppendUint16(buf, qtype)
	buf = binary.BigEndian.AppendUint16(buf, classIN)

	for _, rec := range recs {
		key := strings.ToLower(rec.name)
		if off, ok := offsets[key]; ok {
			buf = append(buf, 0xC0|byte(off>>8), byte(off))
		} else {
			offsets[key] = len(buf)
			ls, err := splitName(rec.name)
			if err != nil {
				return nil, err
			}
			for _, label := range ls {
				buf = append(buf, byte(len(label)))
				buf = append(buf, label...)
			}
			buf = append(buf, 0)
		}
		buf = binary.BigEndian.AppendUint16(buf, rec.typ)
		buf = binary.BigEndian.AppendUint16(buf, classIN)
		buf = binary.BigEndian.AppendUint32(buf, rec.ttl)

		var rdata []byte
		switch rec.typ {
		case typeA:
			rdata = rec.ip.To4()
		case typeAAAA:
			rdata = rec.ip.To16()
		case typeCNAME:
			ls, err := splitName(rec.target)
			if err != nil {
				return nil, err
			}
			for _, label := range ls {
				rdata = append(rdata, byte(len(label)))
				rdata = append(rdata, label...)
			}
			rdata = append(rdata, 0)
			offsets[strings.ToLower(rec.target)] = len(buf) + 2
		}
		buf = binary.BigEndian.AppendUint16(buf, uint16(len(rdata)))
		buf = append(buf, rdata...)
	}
	return buf, nil
}

func buildTestResponse(t *testing.T, id uint16, qname string, qtype uint16, recs []testRecord, rcode int) []byte {
	t.Helper()
	resp, err := buildResponse(id, qname, qtype, recs, rcode)
	if err != nil {
		t.Fatalf("buildResponse: %v", err)
	}
	return resp
}

// responseForQuery 供 HTTP/TCP 处理协程使用：失败时返回 nil，不调用 t.Fatal。
func responseForQuery(query []byte, ips []string, ttl uint32, rcode int) []byte {
	m, err := parseMessage(query)
	if err != nil || len(m.Questions) == 0 {
		return nil
	}
	q := m.Questions[0]
	recs := make([]testRecord, 0, len(ips))
	for _, raw := range ips {
		ip := net.ParseIP(raw)
		if ip == nil {
			return nil
		}
		recs = append(recs, testRecord{name: q.Name, typ: q.Type, ttl: ttl, ip: ip})
	}
	resp, err := buildResponse(m.ID, q.Name, q.Type, recs, rcode)
	if err != nil {
		return nil
	}
	return resp
}

// makeResponseForQuery 在测试主协程使用，失败即终止。
func makeResponseForQuery(t *testing.T, query []byte, ips []string, ttl uint32, rcode int) []byte {
	t.Helper()
	resp := responseForQuery(query, ips, ttl, rcode)
	if resp == nil {
		t.Fatalf("构造响应失败: % x", query)
	}
	return resp
}

// startTCPDNS 启动一个循环应答的 TCP DNS 服务器。
func startTCPDNS(t *testing.T, respond func(query []byte) []byte) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				var length [2]byte
				if _, err := io.ReadFull(conn, length[:]); err != nil {
					return
				}
				query := make([]byte, binary.BigEndian.Uint16(length[:]))
				if _, err := io.ReadFull(conn, query); err != nil {
					return
				}
				resp := respond(query)
				if resp == nil {
					return
				}
				binary.BigEndian.PutUint16(length[:], uint16(len(resp)))
				conn.Write(append(length[:], resp...))
			}()
		}
	}()
	return ln.Addr().String()
}
