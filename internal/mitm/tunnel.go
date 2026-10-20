package mitm

import (
	"context"
	"net"
	"sync"
	"time"
)

const tunnelBufferSize = 32 * 1024

var tunnelBuffers = sync.Pool{
	New: func() any {
		buf := make([]byte, tunnelBufferSize)
		return &buf
	},
}

// Tunnel 在客户端与上游之间双向复制，任一方向结束即关闭两端。
func Tunnel(ctx context.Context, client, upstream net.Conn, idle time.Duration) error {
	done := make(chan error, 2)
	go func() { done <- copyIdle(upstream, client, idle) }()
	go func() { done <- copyIdle(client, upstream, idle) }()

	var first error
	select {
	case first = <-done:
	case <-ctx.Done():
		first = ctx.Err()
	}
	client.Close()
	upstream.Close()
	<-done
	return first
}

func copyIdle(dst, src net.Conn, idle time.Duration) error {
	bufPtr := tunnelBuffers.Get().(*[]byte)
	defer tunnelBuffers.Put(bufPtr)
	buf := *bufPtr
	for {
		if idle > 0 {
			src.SetReadDeadline(time.Now().Add(idle))
		}
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			return err
		}
	}
}
