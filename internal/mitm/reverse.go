package mitm

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"fastgithub-go/internal/config"
)

// ReverseProxy 按域名规则转发请求，支持短路响应与 Destination 改写。
type ReverseProxy struct {
	store     *config.Store
	transport http.RoundTripper
}

func NewReverseProxy(store *config.Store, transport http.RoundTripper) *ReverseProxy {
	return &ReverseProxy{store: store, transport: transport}
}

func (p *ReverseProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	domain, matched := p.store.Lookup(host)
	if matched && domain.Response != nil {
		writeShortCircuit(w, domain.Response)
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
	removeHopByHop(out.Header)

	resp, err := p.transport.RoundTrip(out)
	if err != nil {
		http.Error(w, "上游请求失败: "+err.Error(), http.StatusBadGateway)
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
	io.Copy(w, resp.Body)
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
