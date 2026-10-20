package mitm

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

func TestTunnelEcho(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.Copy(conn, conn)
	}()

	upstream, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client, app := net.Pipe()
	done := make(chan error, 1)
	go func() { done <- Tunnel(context.Background(), client, upstream, time.Minute) }()

	if _, err := app.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(app, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "hello" {
		t.Fatalf("echo = %q", buf)
	}
	app.Close()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Tunnel 未退出")
	}
}

func TestIsTLS(t *testing.T) {
	if !IsTLS([]byte{0x16, 0x03, 0x01}) {
		t.Fatal("TLS 记录应识别")
	}
	if IsTLS([]byte("GET")) || IsTLS([]byte{0x16}) {
		t.Fatal("非 TLS 不应识别")
	}
}
