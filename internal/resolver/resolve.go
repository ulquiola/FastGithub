package resolver

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"fastgithub-go/internal/config"
)

const (
	minCacheTTL          = 30 * time.Second
	maxCacheTTL          = 10 * time.Minute
	nonSocketNegativeTTL = 30 * time.Second
	socketNegativeTTL    = 10 * time.Minute
	racePort             = "443"
	raceTimeout          = 2 * time.Second
)

// Resolver 按 DoH → 明文 DNS（TCP）的顺序解析 A/AAAA，并缓存与竞速排序。
type Resolver struct {
	doh      *dohClient
	fallback []string
	timeout  time.Duration
	cache    *ipCache
	latency  *latencyCache
	group    *flightGroup
	racer    *racer
	now      func() time.Time
}

// New 用 dns.* 配置构造解析器。
func New(cfg config.DNS) *Resolver {
	timeout := cfg.Timeout.Std()
	if timeout <= 0 {
		timeout = 4 * time.Second
	}
	r := &Resolver{
		doh:      &dohClient{endpoints: append([]string(nil), cfg.DoH...), client: &http.Client{Timeout: timeout}},
		fallback: append([]string(nil), cfg.Fallback...),
		timeout:  timeout,
		cache:    newIPCache(4096),
		latency:  newLatencyCache(4096),
		group:    newFlightGroup(),
		now:      time.Now,
	}
	r.racer = &racer{
		timeout: raceTimeout,
		port:    racePort,
		latency: r.latency,
		dial:    (&net.Dialer{}).DialContext,
		now:     func() time.Time { return r.now() },
	}
	return r
}

// Lookup 返回按 TCP 时延排序的 A/AAAA 地址；负缓存命中时返回缓存的错误。
func (r *Resolver) Lookup(ctx context.Context, domain string) ([]net.IP, error) {
	domain = normalizeDomain(domain)
	if domain == "" {
		return nil, fmt.Errorf("域名不能为空")
	}
	if ips, err, ok := r.cache.get(domain, r.now()); ok {
		return ips, err
	}
	ips, err := r.group.Do(domain, func() ([]net.IP, error) {
		if ips, err, ok := r.cache.get(domain, r.now()); ok {
			return ips, err
		}
		return r.resolve(ctx, domain)
	})
	if err != nil {
		return nil, err
	}
	return copyIPs(ips), nil
}

func (r *Resolver) resolve(ctx context.Context, domain string) ([]net.IP, error) {
	type answer struct {
		ips []net.IP
		ttl uint32
		err error
	}
	ch := make(chan answer, 2)
	for _, qtype := range []uint16{typeA, typeAAAA} {
		go func(qt uint16) {
			ips, ttl, err := r.lookupType(ctx, domain, qt)
			ch <- answer{ips: ips, ttl: ttl, err: err}
		}(qtype)
	}

	var ips []net.IP
	var errs []error
	var ttl uint32
	ttlSet := false
	for i := 0; i < 2; i++ {
		a := <-ch
		if a.err != nil {
			errs = append(errs, a.err)
			continue
		}
		ips = append(ips, a.ips...)
		if !ttlSet || a.ttl < ttl {
			ttl, ttlSet = a.ttl, true
		}
	}
	ips = dedupIPs(ips)
	if len(ips) == 0 {
		err := errors.Join(errs...)
		if err == nil {
			err = fmt.Errorf("%s 没有 A/AAAA 记录", domain)
		}
		r.cache.putNegative(domain, err, negativeTTL(err), r.now())
		return nil, err
	}
	ips = r.racer.Sort(ctx, ips)
	r.cache.putPositive(domain, ips, clampTTL(ttl), r.now())
	return ips, nil
}

func (r *Resolver) lookupType(ctx context.Context, domain string, qtype uint16) ([]net.IP, uint32, error) {
	msg, id, err := buildQuery(domain, qtype)
	if err != nil {
		return nil, 0, err
	}
	var errs []error
	for _, endpoint := range r.doh.endpoints {
		attemptCtx, cancel := context.WithTimeout(ctx, r.timeout)
		resp, err := r.doh.query(attemptCtx, endpoint, msg)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("DoH %s: %w", endpoint, err))
			continue
		}
		ips, ttl, err := parseAnswer(resp, id, domain, qtype)
		if err != nil {
			errs = append(errs, fmt.Errorf("DoH %s: %w", endpoint, err))
			if errors.Is(err, errNXDOMAIN) {
				return nil, 0, err
			}
			continue
		}
		return ips, ttl, nil
	}
	for _, server := range r.fallback {
		attemptCtx, cancel := context.WithTimeout(ctx, r.timeout)
		resp, err := queryTCP(attemptCtx, server, msg)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Errorf("DNS %s: %w", server, err))
			continue
		}
		ips, ttl, err := parseAnswer(resp, id, domain, qtype)
		if err != nil {
			errs = append(errs, fmt.Errorf("DNS %s: %w", server, err))
			if errors.Is(err, errNXDOMAIN) {
				return nil, 0, err
			}
			continue
		}
		return ips, ttl, nil
	}
	if len(errs) == 0 {
		return nil, 0, fmt.Errorf("没有可用的 DNS 源")
	}
	return nil, 0, errors.Join(errs...)
}

func normalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimRight(domain, "."))
}

func clampTTL(ttl uint32) time.Duration {
	d := time.Duration(ttl) * time.Second
	if d < minCacheTTL {
		return minCacheTTL
	}
	if d > maxCacheTTL {
		return maxCacheTTL
	}
	return d
}

func negativeTTL(err error) time.Duration {
	var ne net.Error
	if errors.As(err, &ne) {
		return socketNegativeTTL
	}
	return nonSocketNegativeTTL
}

func dedupIPs(ips []net.IP) []net.IP {
	seen := make(map[string]bool, len(ips))
	out := ips[:0]
	for _, ip := range ips {
		key := ip.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ip)
	}
	return out
}

// queryTCP 按 2 字节长度前缀收发 DNS 报文。
func queryTCP(ctx context.Context, server string, msg []byte) ([]byte, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", server)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(msg)))
	if _, err := conn.Write(append(length[:], msg...)); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(conn, length[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(length[:]))
	if n == 0 {
		return nil, fmt.Errorf("DNS 响应为空")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	return body, nil
}

// parseAnswer 校验响应并提取 qtype 对应的地址与 TTL。
func parseAnswer(data []byte, id uint16, domain string, qtype uint16) ([]net.IP, uint32, error) {
	m, err := parseMessage(data)
	if err != nil {
		return nil, 0, err
	}
	if m.ID != id {
		return nil, 0, fmt.Errorf("DNS 响应 ID 不匹配")
	}
	if m.Flags&0x8000 == 0 {
		return nil, 0, fmt.Errorf("DNS 响应缺少 QR 位")
	}
	if len(m.Questions) > 0 && !strings.EqualFold(strings.TrimSuffix(m.Questions[0].Name, "."), domain) {
		return nil, 0, fmt.Errorf("DNS 响应问题名不匹配")
	}
	switch m.rcode() {
	case 0:
	case 3:
		return nil, 0, errNXDOMAIN
	default:
		return nil, 0, fmt.Errorf("DNS 返回错误码 %d", m.rcode())
	}
	ips, ttl := m.addresses(domain, qtype)
	if len(ips) == 0 {
		return nil, 0, fmt.Errorf("无 A/AAAA 记录")
	}
	return ips, ttl, nil
}
