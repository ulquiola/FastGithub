package resolver

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
)

const (
	typeA     = 1
	typeCNAME = 5
	typeAAAA  = 28
	classIN   = 1
)

// errNXDOMAIN 表示域名不存在；权威否定结果不再尝试其他 DNS 源。
var errNXDOMAIN = errors.New("域名不存在 (NXDOMAIN)")

// errNoRecords 表示 NOERROR 但没有对应类型的记录（NODATA），不是端点故障。
var errNoRecords = errors.New("无 A/AAAA 记录")

type dnsQuestion struct {
	Name  string
	Type  uint16
	Class uint16
}

type dnsRecord struct {
	Name   string
	Type   uint16
	Class  uint16
	TTL    uint32
	Target string // CNAME 目标
	IP     net.IP // A/AAAA 地址
}

type dnsMessage struct {
	ID        uint16
	Flags     uint16
	Questions []dnsQuestion
	Answers   []dnsRecord
}

func (m *dnsMessage) rcode() int { return int(m.Flags & 0xF) }

// buildQuery 生成 RD=1 的查询报文，ID 取自 crypto/rand。
func buildQuery(domain string, qtype uint16) ([]byte, uint16, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, 0, fmt.Errorf("生成 DNS 请求 ID 失败: %w", err)
	}
	return buildQueryWithID(domain, qtype, binary.BigEndian.Uint16(b[:]))
}

func buildQueryWithID(domain string, qtype uint16, id uint16) ([]byte, uint16, error) {
	labels, err := splitName(domain)
	if err != nil {
		return nil, 0, err
	}
	msg := make([]byte, 12, 12+len(domain)+6)
	binary.BigEndian.PutUint16(msg[0:2], id)
	binary.BigEndian.PutUint16(msg[2:4], 0x0100) // RD
	binary.BigEndian.PutUint16(msg[4:6], 1)
	for _, label := range labels {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0)
	msg = binary.BigEndian.AppendUint16(msg, qtype)
	msg = binary.BigEndian.AppendUint16(msg, classIN)
	return msg, id, nil
}

func splitName(domain string) ([][]byte, error) {
	domain = strings.TrimSuffix(domain, ".")
	if domain == "" {
		return nil, fmt.Errorf("域名不能为空")
	}
	parts := strings.Split(domain, ".")
	labels := make([][]byte, 0, len(parts))
	total := 1
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("域名包含空标签: %q", domain)
		}
		if len(part) > 63 {
			return nil, fmt.Errorf("域名标签过长: %q", part)
		}
		total += 1 + len(part)
		labels = append(labels, []byte(part))
	}
	if total > 255 {
		return nil, fmt.Errorf("域名过长: %q", domain)
	}
	return labels, nil
}

// parseMessage 解析报文头、问题与答案段；authority/additional 忽略。
func parseMessage(data []byte) (*dnsMessage, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("DNS 报文过短")
	}
	m := &dnsMessage{
		ID:    binary.BigEndian.Uint16(data[0:2]),
		Flags: binary.BigEndian.Uint16(data[2:4]),
	}
	qd := int(binary.BigEndian.Uint16(data[4:6]))
	an := int(binary.BigEndian.Uint16(data[6:8]))
	off := 12
	for i := 0; i < qd; i++ {
		name, next, err := decodeName(data, off)
		if err != nil {
			return nil, err
		}
		if next+4 > len(data) {
			return nil, fmt.Errorf("DNS 问题段截断")
		}
		m.Questions = append(m.Questions, dnsQuestion{
			Name:  name,
			Type:  binary.BigEndian.Uint16(data[next:]),
			Class: binary.BigEndian.Uint16(data[next+2:]),
		})
		off = next + 4
	}
	for i := 0; i < an; i++ {
		name, next, err := decodeName(data, off)
		if err != nil {
			return nil, err
		}
		if next+10 > len(data) {
			return nil, fmt.Errorf("DNS 答案段截断")
		}
		rec := dnsRecord{
			Name:  name,
			Type:  binary.BigEndian.Uint16(data[next:]),
			Class: binary.BigEndian.Uint16(data[next+2:]),
			TTL:   binary.BigEndian.Uint32(data[next+4:]),
		}
		rdlen := int(binary.BigEndian.Uint16(data[next+8:]))
		rdata := next + 10
		if rdata+rdlen > len(data) {
			return nil, fmt.Errorf("DNS 记录数据截断")
		}
		switch rec.Type {
		case typeA:
			if rdlen != 4 {
				return nil, fmt.Errorf("A 记录长度异常: %d", rdlen)
			}
			rec.IP = net.IPv4(data[rdata], data[rdata+1], data[rdata+2], data[rdata+3]).To4()
		case typeAAAA:
			if rdlen != 16 {
				return nil, fmt.Errorf("AAAA 记录长度异常: %d", rdlen)
			}
			rec.IP = append(net.IP(nil), data[rdata:rdata+16]...)
		case typeCNAME:
			target, _, err := decodeName(data, rdata)
			if err != nil {
				return nil, err
			}
			rec.Target = target
		}
		m.Answers = append(m.Answers, rec)
		off = rdata + rdlen
	}
	return m, nil
}

// decodeName 读取域名并处理压缩指针；返回小写、无尾点的名字和下一段偏移。
func decodeName(msg []byte, off int) (string, int, error) {
	var labels []string
	pos := off
	next := -1
	jumps := 0
	nameLen := 1 // 根标签
	for {
		if pos >= len(msg) {
			return "", 0, fmt.Errorf("DNS 名字越界")
		}
		length := int(msg[pos])
		if length == 0 {
			pos++
			break
		}
		if length&0xC0 == 0xC0 {
			if pos+1 >= len(msg) {
				return "", 0, fmt.Errorf("DNS 压缩指针截断")
			}
			if next < 0 {
				next = pos + 2
			}
			jumps++
			if jumps > 128 {
				return "", 0, fmt.Errorf("DNS 压缩指针过多")
			}
			ptr := int(msg[pos]&0x3F)<<8 | int(msg[pos+1])
			if ptr < 12 {
				return "", 0, fmt.Errorf("DNS 压缩指针指向报文头")
			}
			if ptr >= pos {
				return "", 0, fmt.Errorf("DNS 压缩指针必须向前引用")
			}
			pos = ptr
			continue
		}
		if length&0xC0 != 0 {
			return "", 0, fmt.Errorf("不支持的 DNS 标签类型")
		}
		pos++
		if pos+length > len(msg) {
			return "", 0, fmt.Errorf("DNS 标签截断")
		}
		if len(labels) >= 128 {
			return "", 0, fmt.Errorf("DNS 标签过多")
		}
		nameLen += 1 + length
		if nameLen > 255 {
			return "", 0, fmt.Errorf("DNS 名字过长")
		}
		labels = append(labels, string(msg[pos:pos+length]))
		pos += length
	}
	if next < 0 {
		next = pos
	}
	return strings.ToLower(strings.Join(labels, ".")), next, nil
}

// addresses 沿 CNAME 链收集 qtype 对应的地址，返回最小 TTL。
func (m *dnsMessage) addresses(qname string, qtype uint16) ([]net.IP, uint32) {
	byName := make(map[string][]dnsRecord, len(m.Answers))
	for _, rec := range m.Answers {
		key := strings.ToLower(strings.TrimSuffix(rec.Name, "."))
		byName[key] = append(byName[key], rec)
	}
	name := strings.ToLower(strings.TrimSuffix(qname, "."))
	seen := make(map[string]bool, 8)
	var ips []net.IP
	var ttl uint32
	ttlSet := false
	for hop := 0; hop < 8 && !seen[name]; hop++ {
		seen[name] = true
		next := ""
		for _, rec := range byName[name] {
			switch {
			case rec.Type == qtype && rec.IP != nil:
				ips = append(ips, rec.IP)
				if !ttlSet || rec.TTL < ttl {
					ttl, ttlSet = rec.TTL, true
				}
			case rec.Type == typeCNAME && rec.Target != "":
				// CNAME 自身的 TTL 同样约束整条链。
				if !ttlSet || rec.TTL < ttl {
					ttl, ttlSet = rec.TTL, true
				}
				next = strings.ToLower(strings.TrimSuffix(rec.Target, "."))
			}
		}
		if next == "" {
			break
		}
		name = next
	}
	return ips, ttl
}
