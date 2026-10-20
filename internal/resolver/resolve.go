package resolver

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
)

// queryTCP 按 2 字节长度前缀收发 DNS 报文。
func queryTCP(ctx context.Context, server string, msg []byte) ([]byte, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", server)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(msg)))
	if _, err := conn.Write(append(length[:], msg...)); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(conn, length[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(length[:]))
	if n == 0 {
		return nil, fmt.Errorf("DNS 响应为空")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	return body, nil
}

// parseAnswer 校验响应并提取 qtype 对应的地址与 TTL。
func parseAnswer(data []byte, id uint16, domain string, qtype uint16) ([]net.IP, uint32, error) {
	m, err := parseMessage(data)
	if err != nil {
		return nil, 0, err
	}
	if m.ID != id {
		return nil, 0, fmt.Errorf("DNS 响应 ID 不匹配")
	}
	if m.Flags&0x8000 == 0 {
		return nil, 0, fmt.Errorf("DNS 响应缺少 QR 位")
	}
	if len(m.Questions) > 0 && !strings.EqualFold(strings.TrimSuffix(m.Questions[0].Name, "."), domain) {
		return nil, 0, fmt.Errorf("DNS 响应问题名不匹配")
	}
	switch m.rcode() {
	case 0:
	case 3:
		return nil, 0, errNXDOMAIN
	default:
		return nil, 0, fmt.Errorf("DNS 返回错误码 %d", m.rcode())
	}
	ips, ttl := m.addresses(domain, qtype)
	if len(ips) == 0 {
		return nil, 0, fmt.Errorf("无 A/AAAA 记录")
	}
	return ips, ttl, nil
}
