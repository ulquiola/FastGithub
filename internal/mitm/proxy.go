package mitm

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/dialer"
)

const defaultTunnelIdle = 5 * time.Minute

var errNoSNI = errors.New("客户端未提供 SNI")

// Server 在显式代理端口上处理 CONNECT、绝对 URI 与 PAC。
type Server struct {
	store   *config.Store
	dialer  *dialer.Dialer
	ca      *certs.Authority
	reverse http.Handler
	idle    time.Duration
}

func NewServer(store *config.Store, d *dialer.Dialer, ca *certs.Authority, reverse http.Handler, idle time.Duration) *Server {
	if idle <= 0 {
		idle = defaultTunnelIdle
	}
	return &Server{store: store, dialer: d, ca: ca, reverse: reverse, idle: idle}
}

// ServeConn 根据连接首字节分派 CONNECT、透明 TLS 或普通 HTTP。
func (s *Server) ServeConn(ctx context.Context, conn net.Conn) {
	// 首字节读取有超时，避免慢速连接长期占用 goroutine。
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReaderSize(conn, maxConnectHeader+1)
	peek, err := br.Peek(2)
	if err != nil && len(peek) == 0 {
		conn.Close()
		return
	}
	if IsTLS(peek) {
		conn.SetReadDeadline(time.Time{})
		s.serveTransparentTLS(ctx, &bufferedConn{Conn: conn, r: br})
		return
	}
	if isConnect(br) {
		s.serveConnect(ctx, conn, br)
		return
	}
	conn.SetReadDeadline(time.Time{})
	s.serveOneHTTP(&bufferedConn{Conn: conn, r: br}, s)
}

// ServeHTTP 处理 38457 上的普通 HTTP：根路径返回 PAC，绝对 URI 走反向代理。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !r.URL.IsAbs() {
		if r.URL.Path == "/" {
			host := r.Host
			if host == "" {
				host = "127.0.0.1:38457"
			}
			w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
			w.Write(BuildPAC(s.store.Patterns(), host))
			return
		}
		http.NotFound(w, r)
		return
	}
	s.reverse.ServeHTTP(w, r)
}

func isConnect(br *bufio.Reader) bool {
	b, err := br.Peek(7)
	return err == nil && string(b) == "CONNECT"
}

func (s *Server) serveConnect(ctx context.Context, conn net.Conn, br *bufio.Reader) {
	host, port, err := ReadConnect(br)
	if err != nil {
		WriteConnectError(conn, http.StatusBadRequest)
		conn.Close()
		return
	}
	conn.SetReadDeadline(time.Time{})
	domain, matched := s.store.Lookup(host)
	client := &bufferedConn{Conn: conn, r: br}
	if matched {
		if err := WriteConnectEstablished(conn); err != nil {
			conn.Close()
			return
		}
		s.serveMITM(ctx, client, host)
		return
	}
	upstream, err := s.dialer.DialTCP(ctx, dialer.Target{Host: host, Port: port, Domain: domain})
	if err != nil {
		WriteConnectError(conn, http.StatusBadGateway)
		conn.Close()
		return
	}
	if err := WriteConnectEstablished(conn); err != nil {
		upstream.Close()
		conn.Close()
		return
	}
	Tunnel(ctx, client, upstream, s.idle)
}

func (s *Server) serveMITM(ctx context.Context, conn net.Conn, connectHost string) {
	tlsConn := tls.Server(conn, &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			name := hello.ServerName
			if name == "" {
				name = connectHost
			}
			return s.ca.LeafFor(name)
		},
		MinVersion: tls.VersionTLS12,
	})
	handshakeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
		conn.Close()
		return
	}
	s.serveOneHTTP(tlsConn, s.reverse)
}

func (s *Server) serveTransparentTLS(ctx context.Context, conn net.Conn) {
	tlsConn := tls.Server(conn, &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			if hello.ServerName == "" {
				return nil, errNoSNI
			}
			return s.ca.LeafFor(hello.ServerName)
		},
		MinVersion: tls.VersionTLS12,
	})
	handshakeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := tlsConn.HandshakeContext(handshakeCtx); err != nil {
		conn.Close()
		return
	}
	s.serveOneHTTP(tlsConn, s.reverse)
}

// serveOneHTTP 把连接交给一次性 http.Server，并等待连接真正关闭后再返回，
// 这样上层注册表能在正确的生命周期内跟踪连接。连接保持原始类型，
// 否则 http.Server 无法识别 *tls.Conn、r.TLS 会为空。
func (s *Server) serveOneHTTP(conn net.Conn, handler http.Handler) {
	done := make(chan struct{})
	var once sync.Once
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       5 * time.Minute,
		ConnState: func(_ net.Conn, state http.ConnState) {
			if state == http.StateClosed || state == http.StateHijacked {
				once.Do(func() { close(done) })
			}
		},
	}
	go srv.Serve(&singleConnListener{conn: conn})
	<-done
}
