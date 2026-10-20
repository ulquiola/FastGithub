package intercept

import (
	"strings"
	"testing"
)

func TestTCPFilter(t *testing.T) {
	filter := TCPFilter(map[int]int{4443: 443, 8080: 80, 2222: 22, 9418: 9418})
	for _, want := range []string{
		"tcp.DstPort == 443", "tcp.SrcPort == 4443",
		"tcp.DstPort == 80", "tcp.SrcPort == 8080",
		"tcp.DstPort == 22", "tcp.SrcPort == 2222",
		"tcp.DstPort == 9418", "tcp.SrcPort == 9418",
	} {
		if !strings.Contains(filter, want) {
			t.Fatalf("filter = %q，缺少 %q", filter, want)
		}
	}
	if !strings.HasPrefix(filter, "loopback and (") {
		t.Fatalf("filter = %q", filter)
	}
	if got := TCPFilter(nil); got != "loopback and false" {
		t.Fatalf("empty filter = %q", got)
	}
}

func TestRemapTCPPorts(t *testing.T) {
	actualToPreferred := map[int]int{4443: 443, 8080: 80, 2222: 22, 9418: 9418}
	preferredToActual := map[int]int{443: 4443, 80: 8080, 22: 2222, 9418: 9418}
	cases := []struct {
		src, dst         uint16
		wantSrc, wantDst uint16
		changed          bool
	}{
		{50000, 443, 50000, 4443, true},
		{50000, 80, 50000, 8080, true},
		{50000, 22, 50000, 2222, true},
		{50000, 9418, 50000, 9418, true},
		{4443, 50000, 443, 50000, true},
		{2222, 50000, 22, 50000, true},
		{50000, 9999, 50000, 9999, false},
		{9999, 50000, 9999, 50000, false},
	}
	for _, c := range cases {
		src, dst, changed := RemapTCPPorts(c.src, c.dst, actualToPreferred, preferredToActual)
		if src != c.wantSrc || dst != c.wantDst || changed != c.changed {
			t.Fatalf("RemapTCPPorts(%d,%d) = %d,%d,%v，期望 %d,%d,%v",
				c.src, c.dst, src, dst, changed, c.wantSrc, c.wantDst, c.changed)
		}
	}
}
