package mitm

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/dialer"
	"fastgithub-go/internal/flow"
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
	logger  Logger
	stats   http.Handler
	http2   bool
}

func NewServer(store *config.Store, d *dialer.Dialer, ca *certs.Authority, reverse http.Handler, idle time.Duration) *Server {
	if idle <= 0 {
		idle = defaultTunnelIdle
	}
	return &Server{store: store, dialer: d, ca: ca, reverse: reverse, idle: idle}
}

// SetLogger 设置请求日志接收器。
func (s *Server) SetLogger(l Logger) { s.logger = l }

// SetHTTP2 决定是否向客户端通告并承载 HTTP/2。
func (s *Server) SetHTTP2(enabled bool) { s.http2 = enabled }

// ServeTLS 处理已接受的透明 TLS 连接（443 入口）。
func (s *Server) ServeTLS(ctx context.Context, conn net.Conn) {
	s.serveTransparentTLS(ctx, conn)
}

// ServeForward 把已接受的连接转发到固定上游（SSH/Git 透明入口）。
func (s *Server) ServeForward(ctx context.Context, conn net.Conn, host string, port int, method string) {
	domain, _ := s.store.Lookup(host)
	upstream, err := s.dialer.DialTCP(ctx, dialer.Target{Host: host, Port: port, Domain: domain})
	if err != nil {
		if s.logger != nil {
			s.logger.Log(flow.Entry{Time: time.Now(), Domain: host, Method: method, Status: http.StatusBadGateway})
		}
		conn.Close()
		return
	}
	start := time.Now()
	clientCount := &countingConn{Conn: conn}
	Tunnel(ctx, clientCount, upstream, s.idle)
	if s.logger != nil {
		s.logger.Log(flow.Entry{
			Time: time.Now(), Domain: host, Method: method, Status: http.StatusOK,
			Duration: time.Since(start),
			Read:     clientCount.read.Load(),
			Write:    clientCount.write.Load(),
		})
	}
}

// ServeHTTPConn 处理已接受的明文 HTTP 连接（80 入口）。
func (s *Server) ServeHTTPConn(ctx context.Context, conn net.Conn) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !r.URL.IsAbs() && r.URL.Path == "/flowStatistics" && s.stats != nil {
			s.stats.ServeHTTP(w, r)
			return
		}
		s.reverse.ServeHTTP(w, r)
	})
	s.serveOneHTTP(conn, handler)
}

// SetStats 设置 /flowStatistics 的处理器。
func (s *Server) SetStats(h http.Handler) { s.stats = h }

func (s *Server) logConnect(host string, status int) {
	if s.logger == nil {
		return
	}
	s.logger.Log(flow.Entry{Time: time.Now(), Domain: host, Method: "CONNECT", Status: status})
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
		if r.URL.Path == "/flowStatistics" && s.stats != nil {
			s.stats.ServeHTTP(w, r)
			return
		}
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
		s.logConnect("", http.StatusBadRequest)
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
		s.logConnect(host, http.StatusBadGateway)
		conn.Close()
		return
	}
	if err := WriteConnectEstablished(conn); err != nil {
		upstream.Close()
		conn.Close()
		return
	}
	start := time.Now()
	clientCount := &countingConn{Conn: client}
	Tunnel(ctx, clientCount, upstream, s.idle)
	if s.logger != nil {
		s.logger.Log(flow.Entry{
			Time: time.Now(), Domain: host, Method: "CONNECT", Status: http.StatusOK,
			Duration: time.Since(start),
			Read:     clientCount.read.Load(),
			Write:    clientCount.write.Load(),
		})
	}
}

func (s *Server) serveMITM(ctx context.Context, conn net.Conn, connectHost string) {
	s.serveOneTLS(conn, s.serverTLSConfig(connectHost), s.reverse)
}

func (s *Server) serveTransparentTLS(ctx context.Context, conn net.Conn) {
	s.serveOneTLS(conn, s.serverTLSConfig(""), s.reverse)
}

// serverTLSConfig 按 SNI 选择叶子证书；fallbackName 为空时要求客户端必须带 SNI。
func (s *Server) serverTLSConfig(fallbackName string) *tls.Config {
	cfg := &tls.Config{
		GetCertificate: func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
			name := hello.ServerName
			if name == "" {
				name = fallbackName
			}
			if name == "" {
				return nil, errNoSNI
			}
			return s.ca.LeafFor(name)
		},
		MinVersion: tls.VersionTLS12,
	}
	if s.http2 {
		cfg.NextProtos = []string{"h2", "http/1.1"}
	}
	return cfg
}

// serveOneHTTP 服务明文 HTTP 连接（80 入口）。
func (s *Server) serveOneHTTP(conn net.Conn, handler http.Handler) {
	s.serveConn(conn, nil, handler)
}

// serveOneTLS 把原始连接交给 ServeTLS：由它完成握手（GetCertificate 选叶证书）
// 并注册标准库 h2 处理器，这是启用 h2 的唯一标准库路径。
func (s *Server) serveOneTLS(conn net.Conn, cfg *tls.Config, handler http.Handler) {
	s.serveConn(conn, cfg, handler)
}

// serveConn 把连接交给一次性 http.Server，并等待连接真正关闭后再返回，
// 这样上层注册表能在正确的生命周期内跟踪连接。
func (s *Server) serveConn(conn net.Conn, tlsCfg *tls.Config, handler http.Handler) {
	done := make(chan struct{})
	var once sync.Once
	srv := &http.Server{
		Handler:           handler,
		TLSConfig:         tlsCfg,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       5 * time.Minute,
		// 握手失败由客户端感知即可，不要往 stderr 刷屏。
		ErrorLog: log.New(io.Discard, "", 0),
		ConnState: func(_ net.Conn, state http.ConnState) {
			if state == http.StateClosed || state == http.StateHijacked {
				once.Do(func() { close(done) })
			}
		},
	}
	if tlsCfg != nil && !s.http2 {
		// ServeTLS 在 TLSNextProto 为 nil 时会自动追加 h2 并注册处理器；
		// 非 nil 空 map 是关闭它的唯一方式，光去掉 NextProtos 不够。
		srv.TLSNextProto = map[string]func(*http.Server, *tls.Conn, http.Handler){}
	}
	l := &singleConnListener{conn: conn}
	go func() {
		if tlsCfg != nil {
			_ = srv.ServeTLS(l, "", "")
			return
		}
		_ = srv.Serve(l)
	}()
	<-done
}
