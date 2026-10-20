package ui

import (
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"html"
	"io"
	"net"
	"net/http"
	"strings"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/flow"
)

//go:embed assets/index.html assets/app.js assets/style.css
var assets embed.FS

// Options 是 UI 服务的依赖。
type Options struct {
	Config        *config.Config
	Store         *config.Store
	Tracker       *flow.Tracker
	Ring          *flow.Ring
	CA            *certs.Authority
	Token         string
	Version       string
	InstallCert   func(string) error
	UninstallCert func(string) error
	CertStatus    func(string) (certs.TrustStatus, error)
}

// Server 提供内嵌页面、REST、SSE 与兼容端点。
type Server struct {
	opts Options
	mux  *http.ServeMux
}

func New(opts Options) *Server {
	if opts.InstallCert == nil {
		opts.InstallCert = certs.Install
	}
	if opts.UninstallCert == nil {
		opts.UninstallCert = certs.Uninstall
	}
	if opts.CertStatus == nil {
		opts.CertStatus = certs.Status
	}
	s := &Server{opts: opts, mux: http.NewServeMux()}
	s.mux.HandleFunc("/", s.index)
	s.mux.HandleFunc("/app.js", staticAsset("assets/app.js", "application/javascript; charset=utf-8"))
	s.mux.HandleFunc("/style.css", staticAsset("assets/style.css", "text/css; charset=utf-8"))
	s.mux.HandleFunc("/api/status", s.status)
	s.mux.HandleFunc("/api/domains", s.domains)
	s.mux.HandleFunc("/api/logs", s.logs)
	s.mux.HandleFunc("/api/cert", s.cert)
	s.mux.HandleFunc("/api/cert/status", s.certStatus)
	s.mux.HandleFunc("/api/cert/install", s.installCert)
	s.mux.HandleFunc("/api/cert/uninstall", s.uninstallCert)
	s.mux.HandleFunc("/api/intercept", s.intercept)
	s.mux.Handle("/flowStatistics", FlowStatistics(opts.Tracker))
	return s
}

// Handler 只允许本机访问，并统一加上防嵌入头。
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, "仅允许本机访问", http.StatusForbidden)
			return
		}
		w.Header().Set("X-Frame-Options", "DENY")
		s.mux.ServeHTTP(w, r)
	})
}

func loopbackHost(host string) bool {
	name := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		name = h
	}
	if name == "localhost" {
		return true
	}
	ip := net.ParseIP(name)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	data, err := assets.ReadFile("assets/index.html")
	if err != nil {
		http.Error(w, "页面不可用", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	io.WriteString(w, strings.ReplaceAll(string(data), "{{TOKEN}}", html.EscapeString(s.opts.Token)))
}

func requireGET(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func staticAsset(path, contentType string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := assets.ReadFile(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.Write(data)
	}
}

type dnsStatus struct {
	DoH      int `json:"doh"`
	Fallback int `json:"fallback"`
}

type statusResponse struct {
	Version       string    `json:"version"`
	ProxyPort     int       `json:"proxyPort"`
	UIPort        int       `json:"uiPort"`
	Intercept     string    `json:"intercept"`
	CAFingerprint string    `json:"caFingerprint"`
	DNS           dnsStatus `json:"dns"`
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	fingerprint := sha256.Sum256(s.opts.CA.Certificate().Raw)
	writeJSON(w, statusResponse{
		Version:       s.opts.Version,
		ProxyPort:     s.opts.Config.Server.ProxyPort,
		UIPort:        s.opts.Config.Server.UIPort,
		Intercept:     s.opts.Config.Intercept.Mode,
		CAFingerprint: hex.EncodeToString(fingerprint[:]),
		DNS:           dnsStatus{DoH: len(s.opts.Config.DNS.DoH), Fallback: len(s.opts.Config.DNS.Fallback)},
	})
}

type domainJSON struct {
	Pattern string `json:"pattern"`
	Hits    int64  `json:"hits"`
}

func (s *Server) domains(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	stats := s.opts.Store.Stats()
	out := make([]domainJSON, 0, len(stats))
	for _, stat := range stats {
		out = append(out, domainJSON{Pattern: stat.Pattern, Hits: stat.Hits})
	}
	writeJSON(w, out)
}

type logEvent struct {
	Time       string `json:"time"`
	Domain     string `json:"domain"`
	Method     string `json:"method"`
	Status     int    `json:"status"`
	DurationMS int64  `json:"durationMs"`
	Read       int64  `json:"read"`
	Write      int64  `json:"write"`
}

func (s *Server) logs(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE 不可用", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, backlog, cancel := s.opts.Ring.SubscribeWithBacklog()
	defer cancel()
	for _, entry := range backlog {
		writeLogEvent(w, entry)
	}
	flusher.Flush()
	for {
		select {
		case entry := <-ch:
			writeLogEvent(w, entry)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

func writeLogEvent(w io.Writer, e flow.Entry) {
	data, _ := json.Marshal(logEvent{
		Time:       e.Time.Format("15:04:05"),
		Domain:     e.Domain,
		Method:     e.Method,
		Status:     e.Status,
		DurationMS: e.Duration.Milliseconds(),
		Read:       e.Read,
		Write:      e.Write,
	})
	io.WriteString(w, "data: "+string(data)+"\n\n")
}

func (s *Server) cert(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	w.Header().Set("Content-Type", "application/x-pem-file")
	w.Header().Set("Content-Disposition", "attachment; filename=fastgithub.crt")
	w.Write(s.opts.CA.CertPEM())
}

func (s *Server) certStatus(w http.ResponseWriter, r *http.Request) {
	if !requireGET(w, r) {
		return
	}
	status, err := s.opts.CertStatus(s.opts.CA.CertPath())
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"installed": status.Installed, "scope": status.Scope})
}

func (s *Server) installCert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "无效的令牌"})
		return
	}
	if err := s.opts.InstallCert(s.opts.CA.CertPath()); err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) uninstallCert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "无效的令牌"})
		return
	}
	if err := s.opts.UninstallCert(s.opts.CA.CertPath()); err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

func (s *Server) intercept(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if !s.authorized(r) {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"error": "无效的令牌"})
		return
	}
	writeJSONStatus(w, http.StatusNotImplemented, map[string]string{"error": "运行时拦截开关尚未提供，请在配置中设置 intercept.mode"})
}

func (s *Server) authorized(r *http.Request) bool {
	token := r.Header.Get("X-FastGithub-Token")
	return token != "" && subtle.ConstantTimeCompare([]byte(token), []byte(s.opts.Token)) == 1
}

// FlowStatistics 返回兼容原版的 PascalCase 流量字段。
func FlowStatistics(tracker *flow.Tracker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		snapshot := tracker.Snapshot()
		writeJSON(w, struct {
			TotalRead  int64 `json:"TotalRead"`
			TotalWrite int64 `json:"TotalWrite"`
			ReadRate   int64 `json:"ReadRate"`
			WriteRate  int64 `json:"WriteRate"`
		}{snapshot.TotalRead, snapshot.TotalWrite, snapshot.ReadRate, snapshot.WriteRate})
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func writeJSONStatus(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
