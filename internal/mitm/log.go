package mitm

import (
	"io"
	"net"
	"net/http"
	"sync/atomic"

	"fastgithub-go/internal/flow"
)

// Logger 接收请求日志。
type Logger interface {
	Log(flow.Entry)
}

type countingReader struct {
	r io.Reader
	n atomic.Int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n.Add(int64(n))
	return n, err
}

func (r *countingReader) Close() error {
	if closer, ok := r.r.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

type countingWriter struct {
	http.ResponseWriter
	n atomic.Int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	w.n.Add(int64(n))
	return n, err
}

type countingConn struct {
	net.Conn
	read  atomic.Int64
	write atomic.Int64
}

func (c *countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.read.Add(int64(n))
	return n, err
}

func (c *countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.write.Add(int64(n))
	return n, err
}
