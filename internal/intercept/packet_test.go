package intercept

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
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

func TestGoldenIPv4UDPChecksum(t *testing.T) {
	want, err := hex.DecodeString("45000021000000004011a912c0a801020808080814e90035000dd52968656c6c6f")
	if err != nil {
		t.Fatal(err)
	}
	got := append([]byte(nil), want...)
	binary.BigEndian.PutUint16(got[10:12], 0)
	binary.BigEndian.PutUint16(got[26:28], 0)
	if err := RecalculateChecksums(got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got  = % x\nwant = % x", got, want)
	}
	p, err := ParsePacket(want)
	if err != nil {
		t.Fatal(err)
	}
	if p.SrcPort != 5353 || p.DstPort != 53 || string(p.Payload) != "hello" {
		t.Fatalf("packet = %+v", p)
	}
}

func TestGoldenIPv6UDPChecksum(t *testing.T) {
	want, err := hex.DecodeString("60000000000d110020010db800000000000000000000000120010db800000000000000000000000204d20035000d5b8668656c6c6f")
	if err != nil {
		t.Fatal(err)
	}
	got := append([]byte(nil), want...)
	binary.BigEndian.PutUint16(got[46:48], 0)
	if err := RecalculateChecksums(got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got  = % x\nwant = % x", got, want)
	}
}

func TestParseIPv6ExtensionHeader(t *testing.T) {
	payload := []byte("hi")
	udpLen := 8 + len(payload)
	total := 40 + 8 + udpLen
	raw := make([]byte, total)
	raw[0] = 0x60
	binary.BigEndian.PutUint16(raw[4:6], uint16(8+udpLen))
	raw[6] = 0 // Hop-by-Hop
	copy(raw[8:24], net.ParseIP("2001:db8::1").To16())
	copy(raw[24:40], net.ParseIP("2001:db8::2").To16())
	raw[40] = protoUDP // 扩展头的 next header
	raw[41] = 0        // 长度 8 字节
	binary.BigEndian.PutUint16(raw[48:], 1234)
	binary.BigEndian.PutUint16(raw[50:], 53)
	binary.BigEndian.PutUint16(raw[52:], uint16(udpLen))
	copy(raw[56:], payload)
	if err := RecalculateChecksums(raw); err != nil {
		t.Fatal(err)
	}
	p, err := ParsePacket(raw)
	if err != nil {
		t.Fatal(err)
	}
	if p.TransportOff != 48 || p.DstPort != 53 || string(p.Payload) != "hi" {
		t.Fatalf("packet = %+v", p)
	}
}

func TestRebuildUDPIPv6(t *testing.T) {
	query := make([]byte, 40+8+3)
	query[0] = 0x60
	binary.BigEndian.PutUint16(query[4:6], 8+3)
	query[6] = protoUDP
	copy(query[8:24], net.ParseIP("2001:db8::1").To16())
	copy(query[24:40], net.ParseIP("2001:db8::2").To16())
	binary.BigEndian.PutUint16(query[40:], 1234)
	binary.BigEndian.PutUint16(query[42:], 53)
	binary.BigEndian.PutUint16(query[44:], 8+3)
	copy(query[48:], "abc")
	if err := RecalculateChecksums(query); err != nil {
		t.Fatal(err)
	}
	out, err := RebuildUDP(query, []byte("answer"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := ParsePacket(out)
	if err != nil {
		t.Fatal(err)
	}
	if !p.SrcIP.Equal(net.ParseIP("2001:db8::2")) || !p.DstIP.Equal(net.ParseIP("2001:db8::1")) {
		t.Fatalf("ips = %v -> %v", p.SrcIP, p.DstIP)
	}
	if p.SrcPort != 53 || p.DstPort != 1234 || string(p.Payload) != "answer" {
		t.Fatalf("packet = %+v", p)
	}
	if binary.BigEndian.Uint16(out[4:6]) != uint16(8+len("answer")) {
		t.Fatalf("IPv6 payload length = %d", binary.BigEndian.Uint16(out[4:6]))
	}
}

func TestParseRejectsIPv6FragmentMBit(t *testing.T) {
	payload := []byte("hi")
	udpLen := 8 + len(payload)
	total := 40 + 8 + udpLen
	raw := make([]byte, total)
	raw[0] = 0x60
	binary.BigEndian.PutUint16(raw[4:6], uint16(8+udpLen))
	raw[6] = 44 // Fragment
	copy(raw[8:24], net.ParseIP("2001:db8::1").To16())
	copy(raw[24:40], net.ParseIP("2001:db8::2").To16())
	raw[40] = protoUDP
	raw[41] = 0
	binary.BigEndian.PutUint16(raw[42:44], 0x0001) // M=1, offset=0
	if _, err := ParsePacket(raw); !errors.Is(err, ErrFragment) {
		t.Fatalf("err = %v，期望 ErrFragment", err)
	}

	// atomic fragment（M=0, offset=0）应可解析。
	binary.BigEndian.PutUint16(raw[42:44], 0)
	binary.BigEndian.PutUint16(raw[48:], 1234)
	binary.BigEndian.PutUint16(raw[50:], 53)
	binary.BigEndian.PutUint16(raw[52:], uint16(udpLen))
	copy(raw[56:], payload)
	p, err := ParsePacket(raw)
	if err != nil {
		t.Fatalf("atomic fragment 应可解析: %v", err)
	}
	if p.TransportOff != 48 || p.DstPort != 53 {
		t.Fatalf("packet = %+v", p)
	}
}

func TestParseRejectsIPv4FirstFragment(t *testing.T) {
	raw := buildIPv4UDP(net.ParseIP("1.1.1.1"), net.ParseIP("2.2.2.2"), 1, 53, nil)
	binary.BigEndian.PutUint16(raw[6:8], 0x2000) // MF=1, offset=0
	if _, err := ParsePacket(raw); !errors.Is(err, ErrFragment) {
		t.Fatalf("err = %v，期望 ErrFragment", err)
	}
}
