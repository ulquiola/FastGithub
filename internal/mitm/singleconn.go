package mitm

import (
	"bufio"
	"io"
	"net"
	"sync"
)

// singleConnListener 只接受一个连接，供一次性 http.Server 使用。
type singleConnListener struct {
	conn net.Conn
	mu   sync.Mutex
	done bool
}

func (l *singleConnListener) Accept() (net.Conn, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.done {
		return nil, io.EOF
	}
	l.done = true
	return l.conn, nil
}

func (l *singleConnListener) Close() error   { return nil }
func (l *singleConnListener) Addr() net.Addr { return l.conn.LocalAddr() }

// bufferedConn 让 http.Server 读取 bufio.Reader 中已预读的字节。
type bufferedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.r.Read(p) }
