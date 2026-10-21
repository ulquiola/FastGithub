package mitm

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"fastgithub-go/internal/config"
	"fastgithub-go/internal/flow"
)

// ReverseProxy 按域名规则转发请求，支持短路响应与 Destination 改写。
type ReverseProxy struct {
	store     *config.Store
	transport http.RoundTripper
	upgrade   http.RoundTripper
	logger    Logger
}

func NewReverseProxy(store *config.Store, transport http.RoundTripper) *ReverseProxy {
	return &ReverseProxy{store: store, transport: transport}
}

// SetLogger 设置请求日志接收器。
func (p *ReverseProxy) SetLogger(l Logger) { p.logger = l }

// SetUpgradeTransport 设置承载协议升级（WebSocket）的 h1 专用 transport。
func (p *ReverseProxy) SetUpgradeTransport(t http.RoundTripper) { p.upgrade = t }

func (p *ReverseProxy) log(host, method string, status int, start time.Time, read, write int64) {
	if p.logger == nil {
		return
	}
	p.logger.Log(flow.Entry{
		Time: time.Now(), Domain: host, Method: method, Status: status,
		Duration: time.Since(start), Read: read, Write: write,
	})
}

func (p *ReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	domain, matched := p.store.Lookup(host)
	if matched && domain.Response != nil {
		writeShortCircuit(w, domain.Response)
		p.log(host, r.Method, domain.Response.Status, start, 0, int64(len(domain.Response.Content)))
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if r.URL.Scheme != "" {
		scheme = r.URL.Scheme
	}
	destination := ""
	if matched {
		destination = domain.Destination
	}
	// 保留请求里的非默认端口（CONNECT 目标可能不是 443）。
	upstreamHost := host
	if _, port, err := net.SplitHostPort(r.Host); err == nil && port != "" {
		upstreamHost = net.JoinHostPort(host, port)
	}
	prefix, err := config.JoinDestination(scheme, upstreamHost, destination)
	if err != nil {
		http.Error(w, "无效的 destination: "+err.Error(), http.StatusBadGateway)
		p.log(host, r.Method, http.StatusBadGateway, start, 0, 0)
		return
	}

	out := r.Clone(r.Context())
	u := *prefix
	u.Path = joinPath(prefix.Path, r.URL.Path)
	// 保留 %2F 等编码，避免上游把路径分隔符语义改掉。
	if raw := joinPath(prefix.EscapedPath(), r.URL.EscapedPath()); raw != u.Path {
		// RawPath 只有在能解码回 Path 时才有效，否则 Go 会静默忽略它。
		if decoded, err := url.PathUnescape(raw); err == nil && decoded == u.Path {
			u.RawPath = raw
		} else {
			u.RawPath = ""
		}
	} else {
		u.RawPath = ""
	}
	u.RawQuery = r.URL.RawQuery
	out.URL = &u
	out.RequestURI = ""
	out.Host = r.Host
	// Upgrade 请求必须保留 Connection/Upgrade，否则上游不会切换协议。
	upgrade := out.Header.Get("Upgrade") != ""
	if upgrade {
		removeHopByHopKeepUpgrade(out.Header)
	} else {
		removeHopByHop(out.Header)
	}
	// 无 body 的请求保持 http.NoBody，否则 Transport 会改成 chunked。
	var requestBody *countingReader
	if out.Body != nil && out.Body != http.NoBody {
		requestBody = &countingReader{r: out.Body}
		out.Body = requestBody
	}
	readBytes := func() int64 {
		if requestBody == nil {
			return 0
		}
		return requestBody.n.Load()
	}

	transport := p.transport
	if upgrade && p.upgrade != nil {
		// h2 不支持 101，升级请求固定走 h1 出口。
		transport = p.upgrade
	}
	resp, err := transport.RoundTrip(out)
	if err != nil {
		http.Error(w, "上游请求失败: "+err.Error(), http.StatusBadGateway)
		p.log(host, r.Method, http.StatusBadGateway, start, readBytes(), 0)
		return
	}
	if resp.StatusCode == http.StatusSwitchingProtocols {
		p.serveUpgrade(w, resp)
		p.log(host, r.Method, resp.StatusCode, start, readBytes(), 0)
		return
	}
	defer resp.Body.Close()
	removeHopByHop(resp.Header)
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	responseBody := &countingWriter{ResponseWriter: w}
	io.Copy(responseBody, resp.Body)
	p.log(host, r.Method, resp.StatusCode, start, readBytes(), responseBody.n.Load())
}

// serveUpgrade 把 101 响应写回客户端，并在客户端与上游之间双向透传字节。
func (p *ReverseProxy) serveUpgrade(w http.ResponseWriter, resp *http.Response) {
	back, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		http.Error(w, "上游未返回可升级连接", http.StatusBadGateway)
		return
	}
	defer back.Close()
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "当前服务器不支持连接劫持", http.StatusInternalServerError)
		return
	}
	client, brw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	resp.Header.Del("Transfer-Encoding")
	resp.Header.Del("Content-Length")
	// 不能直接用 resp.Write：101 响应 ContentLength 为 0 时，它会先读一个
	// body 字节判断是否真的为空，而升级后的 body 要等客户端先发数据，会死锁。
	proto := "HTTP/1.1"
	if resp.ProtoMajor > 0 {
		proto = fmt.Sprintf("HTTP/%d.%d", resp.ProtoMajor, resp.ProtoMinor)
	}
	text := http.StatusText(resp.StatusCode)
	if _, err := fmt.Fprintf(client, "%s %d %s\r\n", proto, resp.StatusCode, text); err != nil {
		return
	}
	if err := resp.Header.Write(client); err != nil {
		return
	}
	if _, err := io.WriteString(client, "\r\n"); err != nil {
		return
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(back, brw); done <- struct{}{} }()
	go func() { _, _ = io.Copy(client, back); done <- struct{}{} }()
	<-done
}

func writeShortCircuit(w http.ResponseWriter, resp *config.Response) {
	if resp.ContentType != "" {
		w.Header().Set("Content-Type", resp.ContentType)
	}
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	io.WriteString(w, resp.Content)
}

func joinPath(base, path string) string {
	switch {
	case base == "" || base == "/":
		return path
	case strings.HasSuffix(base, "/") && strings.HasPrefix(path, "/"):
		return base + path[1:]
	case !strings.HasSuffix(base, "/") && !strings.HasPrefix(path, "/"):
		return base + "/" + path
	default:
		return base + path
	}
}

var hopByHopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func removeHopByHop(h http.Header) {
	for _, value := range h.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			h.Del(strings.TrimSpace(name))
		}
	}
	for _, name := range hopByHopHeaders {
		h.Del(name)
	}
}

// removeHopByHopKeepUpgrade 与 removeHopByHop 相同，但保留协议升级所需的头。
func removeHopByHopKeepUpgrade(h http.Header) {
	for _, value := range h.Values("Connection") {
		for _, name := range strings.Split(value, ",") {
			name = strings.TrimSpace(name)
			if strings.EqualFold(name, "upgrade") {
				continue
			}
			h.Del(name)
		}
	}
	for _, name := range hopByHopHeaders {
		if strings.EqualFold(name, "Upgrade") {
			continue
		}
		h.Del(name)
	}
	h.Set("Connection", "Upgrade")
}
