package intercept

import (
	"encoding/binary"
	"errors"
	"net"
	"testing"
)

// buildIPv4UDP 构造一个校验和正确的 UDP 包。
func buildIPv4UDP(src, dst net.IP, srcPort, dstPort uint16, payload []byte) []byte {
	udpLen := 8 + len(payload)
	total := 20 + udpLen
	raw := make([]byte, total)
	raw[0] = 0x45
	binary.BigEndian.PutUint16(raw[2:4], uint16(total))
	raw[8] = 64
	raw[9] = protoUDP
	copy(raw[12:16], src.To4())
	copy(raw[16:20], dst.To4())
	binary.BigEndian.PutUint16(raw[20:], srcPort)
	binary.BigEndian.PutUint16(raw[22:], dstPort)
	binary.BigEndian.PutUint16(raw[24:], uint16(udpLen))
	copy(raw[28:], payload)
	_ = RecalculateChecksums(raw)
	return raw
}

func TestParseIPv4UDP(t *testing.T) {
	raw := buildIPv4UDP(net.ParseIP("192.168.1.2"), net.ParseIP("8.8.8.8"), 5353, 53, []byte("x"))
	p, err := ParsePacket(raw)
	if err != nil {
		t.Fatalf("ParsePacket: %v", err)
	}
	if p.Version != 4 || p.Protocol != protoUDP || p.SrcPort != 5353 || p.DstPort != 53 {
		t.Fatalf("packet = %+v", p)
	}
	if !p.SrcIP.Equal(net.ParseIP("192.168.1.2")) || !p.DstIP.Equal(net.ParseIP("8.8.8.8")) {
		t.Fatalf("ips = %v -> %v", p.SrcIP, p.DstIP)
	}
	if string(p.Payload) != "x" {
		t.Fatalf("payload = %q", p.Payload)
	}
}

func TestParseRejectsBadPackets(t *testing.T) {
	cases := [][]byte{
		{},
		{0x60, 0, 0, 0}, // IPv6 头过短
	}
	for _, raw := range cases {
		if _, err := ParsePacket(raw); err == nil {
			t.Fatalf("ParsePacket(% x) 应报错", raw)
		}
	}
	// IPv4 非首片。
	raw := buildIPv4UDP(net.ParseIP("1.1.1.1"), net.ParseIP("2.2.2.2"), 1, 53, nil)
	binary.BigEndian.PutUint16(raw[6:8], 0x2005)
	if _, err := ParsePacket(raw); !errors.Is(err, ErrFragment) {
		t.Fatalf("err = %v，期望 ErrFragment", err)
	}
}

func TestParseIPv6UDP(t *testing.T) {
	payload := []byte("hello")
	udpLen := 8 + len(payload)
	raw := make([]byte, 40+udpLen)
	raw[0] = 0x60
	binary.BigEndian.PutUint16(raw[4:6], uint16(udpLen))
	raw[6] = protoUDP
	copy(raw[8:24], net.ParseIP("2001:db8::1").To16())
	copy(raw[24:40], net.ParseIP("2001:db8::2").To16())
	binary.BigEndian.PutUint16(raw[40:], 1234)
	binary.BigEndian.PutUint16(raw[42:], 53)
	binary.BigEndian.PutUint16(raw[44:], uint16(udpLen))
	copy(raw[48:], payload)
	_ = RecalculateChecksums(raw)

	p, err := ParsePacket(raw)
	if err != nil {
		t.Fatalf("ParsePacket: %v", err)
	}
	if p.Version != 6 || p.DstPort != 53 || p.SrcPort != 1234 {
		t.Fatalf("packet = %+v", p)
	}
}

func TestSetPortsAndChecksum(t *testing.T) {
	raw := buildIPv4UDP(net.ParseIP("10.0.0.1"), net.ParseIP("127.0.0.1"), 1234, 443, []byte("data"))
	p, err := ParsePacket(raw)
	if err != nil {
		t.Fatal(err)
	}
	p.SetDstPort(38457)
	if p.DstPort != 38457 {
		t.Fatalf("DstPort = %d", p.DstPort)
	}
	if err := RecalculateChecksums(raw); err != nil {
		t.Fatal(err)
	}
	q, err := ParsePacket(raw)
	if err != nil {
		t.Fatal(err)
	}
	if verifyUDP(q) {
		return
	}
	t.Fatal("改端口后校验和应有效")
}

func verifyUDP(p *Packet) bool {
	segment := make([]byte, len(p.Raw)-p.TransportOff)
	copy(segment, p.Raw[p.TransportOff:])
	if p.Protocol != protoUDP {
		return true
	}
	want := transportChecksum(p, segment)
	return want == 0 || ^want == binary.BigEndian.Uint16(p.Raw[p.TransportOff+6:])
}

func TestRebuildUDPSwaps(t *testing.T) {
	raw := buildIPv4UDP(net.ParseIP("192.168.1.2"), net.ParseIP("8.8.8.8"), 5353, 53, []byte("query"))
	out, err := RebuildUDP(raw, []byte("answer"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParsePacket(out)
	if err != nil {
		t.Fatal(err)
	}
	if !p.SrcIP.Equal(net.ParseIP("8.8.8.8")) || !p.DstIP.Equal(net.ParseIP("192.168.1.2")) {
		t.Fatalf("ips = %v -> %v", p.SrcIP, p.DstIP)
	}
	if p.SrcPort != 53 || p.DstPort != 5353 {
		t.Fatalf("ports = %d -> %d", p.SrcPort, p.DstPort)
	}
	if string(p.Payload) != "answer" {
		t.Fatalf("payload = %q", p.Payload)
	}
	if !verifyUDP(p) {
		t.Fatal("重建包的 UDP 校验和无效")
	}
}

func TestParseTCP(t *testing.T) {
	total := 20 + 20 + 4
	raw := make([]byte, total)
	raw[0] = 0x45
	binary.BigEndian.PutUint16(raw[2:4], uint16(total))
	raw[9] = protoTCP
	copy(raw[12:16], net.ParseIP("127.0.0.1").To4())
	copy(raw[16:20], net.ParseIP("127.0.0.1").To4())
	binary.BigEndian.PutUint16(raw[20:], 51000)
	binary.BigEndian.PutUint16(raw[22:], 443)
	raw[32] = 0x50 // data offset 5
	copy(raw[40:], "abcd")
	_ = RecalculateChecksums(raw)

	p, err := ParsePacket(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.Protocol != protoTCP || p.SrcPort != 51000 || p.DstPort != 443 || string(p.Payload) != "abcd" {
		t.Fatalf("packet = %+v", p)
	}
	p.SetSrcPort(443)
	p.SetDstPort(51000)
	if err := RecalculateChecksums(raw); err != nil {
		t.Fatal(err)
	}
	if _, err := ParsePacket(raw); err != nil {
		t.Fatal(err)
	}
}
