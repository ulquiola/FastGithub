package intercept

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

// ErrFragment 表示非首片分片，调用方应原样回注。
var ErrFragment = errors.New("IP 分片")

const (
	protoTCP = 6
	protoUDP = 17
)

// Packet 是解析后的 IP 包视图。
type Packet struct {
	Raw          []byte
	Version      int
	HeaderLen    int
	TransportOff int
	Protocol     uint8
	SrcIP        net.IP
	DstIP        net.IP
	SrcPort      uint16
	DstPort      uint16
	Payload      []byte
}

func ParsePacket(raw []byte) (*Packet, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("空包")
	}
	switch raw[0] >> 4 {
	case 4:
		return parseIPv4(raw)
	case 6:
		return parseIPv6(raw)
	default:
		return nil, fmt.Errorf("不支持的 IP 版本 %d", raw[0]>>4)
	}
}

func parseIPv4(raw []byte) (*Packet, error) {
	if len(raw) < 20 {
		return nil, fmt.Errorf("IPv4 头过短")
	}
	ihl := int(raw[0]&0x0F) * 4
	if ihl < 20 || ihl > len(raw) {
		return nil, fmt.Errorf("IPv4 头长度非法")
	}
	total := int(binary.BigEndian.Uint16(raw[2:4]))
	if total < ihl || total > len(raw) {
		return nil, fmt.Errorf("IPv4 总长度非法")
	}
	if binary.BigEndian.Uint16(raw[6:8])&0x1FFF != 0 {
		return nil, ErrFragment
	}
	p := &Packet{
		Raw:          raw[:total],
		Version:      4,
		HeaderLen:    ihl,
		TransportOff: ihl,
		Protocol:     raw[9],
		SrcIP:        append(net.IP(nil), raw[12:16]...),
		DstIP:        append(net.IP(nil), raw[16:20]...),
	}
	return parseTransport(p)
}

func parseIPv6(raw []byte) (*Packet, error) {
	if len(raw) < 40 {
		return nil, fmt.Errorf("IPv6 头过短")
	}
	payloadLen := int(binary.BigEndian.Uint16(raw[4:6]))
	total := 40 + payloadLen
	if total > len(raw) {
		return nil, fmt.Errorf("IPv6 总长度非法")
	}
	next := raw[6]
	off := 40
	for i := 0; i < 8; i++ {
		switch next {
		case 0, 43, 60: // Hop-by-Hop、Routing、Destination Options
			if off+2 > total {
				return nil, fmt.Errorf("IPv6 扩展头截断")
			}
			length := (int(raw[off+1]) + 1) * 8
			if off+length > total {
				return nil, fmt.Errorf("IPv6 扩展头越界")
			}
			next = raw[off]
			off += length
		case 44: // Fragment
			if off+8 > total {
				return nil, fmt.Errorf("IPv6 分片头截断")
			}
			if binary.BigEndian.Uint16(raw[off+2:off+4])&0xFFF8 != 0 {
				return nil, ErrFragment
			}
			next = raw[off]
			off += 8
		default:
			p := &Packet{
				Raw:          raw[:total],
				Version:      6,
				HeaderLen:    off,
				TransportOff: off,
				Protocol:     next,
				SrcIP:        append(net.IP(nil), raw[8:24]...),
				DstIP:        append(net.IP(nil), raw[24:40]...),
			}
			return parseTransport(p)
		}
	}
	return nil, fmt.Errorf("IPv6 扩展头过多")
}

func parseTransport(p *Packet) (*Packet, error) {
	raw := p.Raw
	off := p.TransportOff
	switch p.Protocol {
	case protoUDP:
		if off+8 > len(raw) {
			return nil, fmt.Errorf("UDP 头截断")
		}
		p.SrcPort = binary.BigEndian.Uint16(raw[off:])
		p.DstPort = binary.BigEndian.Uint16(raw[off+2:])
		length := int(binary.BigEndian.Uint16(raw[off+4:]))
		if length < 8 || off+length > len(raw) {
			return nil, fmt.Errorf("UDP 长度非法")
		}
		p.Payload = raw[off+8 : off+length]
	case protoTCP:
		if off+20 > len(raw) {
			return nil, fmt.Errorf("TCP 头截断")
		}
		p.SrcPort = binary.BigEndian.Uint16(raw[off:])
		p.DstPort = binary.BigEndian.Uint16(raw[off+2:])
		headerLen := int(raw[off+12]>>4) * 4
		if headerLen < 20 || off+headerLen > len(raw) {
			return nil, fmt.Errorf("TCP 头长度非法")
		}
		p.Payload = raw[off+headerLen:]
	default:
		// 非 TCP/UDP 包只解析 IP 层，调用方原样回注。
	}
	return p, nil
}

func (p *Packet) SetDstPort(port uint16) {
	binary.BigEndian.PutUint16(p.Raw[p.TransportOff+2:], port)
	p.DstPort = port
}

func (p *Packet) SetSrcPort(port uint16) {
	binary.BigEndian.PutUint16(p.Raw[p.TransportOff:], port)
	p.SrcPort = port
}

// SwapAddresses 交换源/目的 IP 与端口，用于构造 DNS 应答。
func (p *Packet) SwapAddresses() {
	if p.Version == 4 {
		swap(p.Raw[12:16], p.Raw[16:20])
	} else {
		swap(p.Raw[8:24], p.Raw[24:40])
	}
	p.SrcIP, p.DstIP = p.DstIP, p.SrcIP
	src := binary.BigEndian.Uint16(p.Raw[p.TransportOff:])
	dst := binary.BigEndian.Uint16(p.Raw[p.TransportOff+2:])
	binary.BigEndian.PutUint16(p.Raw[p.TransportOff:], dst)
	binary.BigEndian.PutUint16(p.Raw[p.TransportOff+2:], src)
	p.SrcPort, p.DstPort = p.DstPort, p.SrcPort
}

func swap(a, b []byte) {
	for i := range a {
		a[i], b[i] = b[i], a[i]
	}
}

// RebuildUDP 用新 payload 重建 UDP 包，并交换地址与端口。
func RebuildUDP(raw []byte, payload []byte) ([]byte, error) {
	p, err := ParsePacket(raw)
	if err != nil {
		return nil, err
	}
	if p.Protocol != protoUDP {
		return nil, fmt.Errorf("不是 UDP 包")
	}
	length := p.TransportOff + 8 + len(payload)
	out := make([]byte, length)
	copy(out, p.Raw[:p.TransportOff+8])
	copy(out[p.TransportOff+8:], payload)
	if p.Version == 4 {
		binary.BigEndian.PutUint16(out[2:4], uint16(length))
	} else {
		binary.BigEndian.PutUint16(out[4:6], uint16(length-40))
	}
	binary.BigEndian.PutUint16(out[p.TransportOff+4:], uint16(8+len(payload)))
	q := &Packet{
		Raw:          out,
		Version:      p.Version,
		HeaderLen:    p.HeaderLen,
		TransportOff: p.TransportOff,
		Protocol:     protoUDP,
		SrcIP:        p.SrcIP,
		DstIP:        p.DstIP,
		SrcPort:      p.SrcPort,
		DstPort:      p.DstPort,
		Payload:      out[p.TransportOff+8:],
	}
	q.SwapAddresses()
	if err := RecalculateChecksums(out); err != nil {
		return nil, err
	}
	return out, nil
}

// RecalculateChecksums 重算 IPv4 头与 UDP/TCP 校验和。
func RecalculateChecksums(raw []byte) error {
	p, err := ParsePacket(raw)
	if err != nil {
		return err
	}
	if p.Version == 4 {
		binary.BigEndian.PutUint16(p.Raw[10:12], 0)
		binary.BigEndian.PutUint16(p.Raw[10:12], checksum(p.Raw[:p.HeaderLen]))
	}
	switch p.Protocol {
	case protoUDP:
		binary.BigEndian.PutUint16(p.Raw[p.TransportOff+6:], 0)
		sum := transportChecksum(p, p.Raw[p.TransportOff:])
		if sum == 0 {
			sum = 0xFFFF
		}
		binary.BigEndian.PutUint16(p.Raw[p.TransportOff+6:], sum)
	case protoTCP:
		binary.BigEndian.PutUint16(p.Raw[p.TransportOff+16:], 0)
		sum := transportChecksum(p, p.Raw[p.TransportOff:])
		binary.BigEndian.PutUint16(p.Raw[p.TransportOff+16:], sum)
	}
	return nil
}

func transportChecksum(p *Packet, segment []byte) uint16 {
	var pseudo []byte
	if p.Version == 4 {
		pseudo = make([]byte, 12)
		copy(pseudo[0:4], p.SrcIP.To4())
		copy(pseudo[4:8], p.DstIP.To4())
		pseudo[9] = p.Protocol
		binary.BigEndian.PutUint16(pseudo[10:12], uint16(len(segment)))
	} else {
		pseudo = make([]byte, 40)
		copy(pseudo[0:16], p.SrcIP.To16())
		copy(pseudo[16:32], p.DstIP.To16())
		binary.BigEndian.PutUint32(pseudo[32:36], uint32(len(segment)))
		pseudo[39] = p.Protocol
	}
	return checksum(append(pseudo, segment...))
}

// checksum 返回 RFC 1071 的 16 位反码和。
func checksum(data []byte) uint16 {
	var sum uint32
	for len(data) >= 2 {
		sum += uint32(binary.BigEndian.Uint16(data))
		data = data[2:]
	}
	if len(data) == 1 {
		sum += uint32(data[0]) << 8
	}
	for sum>>16 != 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	return ^uint16(sum)
}
