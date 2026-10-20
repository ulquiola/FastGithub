package app

import (
	"net"
	"testing"
)

func TestListenTCPFallbackSkipsBusyPort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port

	ln, actual, err := ListenTCPFallback("127.0.0.1", port, 3)
	if err != nil {
		t.Fatalf("ListenTCPFallback: %v", err)
	}
	defer ln.Close()
	if actual <= port || actual > port+2 {
		t.Fatalf("actual = %d，期望在 %d..%d 之间", actual, port+1, port+2)
	}
}

func TestListenTCPFallbackRandom(t *testing.T) {
	ln, port, err := ListenTCPFallback("127.0.0.1", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if port == 0 {
		t.Fatal("未返回实际端口")
	}
}
