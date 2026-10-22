package resolver

import (
	"context"
	"net"
	"sort"
	"sync"
	"time"
)

// racer 对候选 IP 并发拨号并按握手耗时排序；失败项排在末尾。
type racer struct {
	timeout time.Duration
	latency *latencyCache
	dial    func(context.Context, string, string) (net.Conn, error)
	now     func() time.Time
}

const maxRaceCandidates = 32

// Sort 按 port 对候选 IP 做 TCP 竞速；时延缓存按 ip:port 区分。
func (r *racer) Sort(ctx context.Context, port string, ips []net.IP) []net.IP {
	now := r.now()
	type probe struct {
		idx     int
		ip      net.IP
		latency time.Duration
		ok      bool
	}
	probeCount := len(ips)
	if probeCount > maxRaceCandidates {
		probeCount = maxRaceCandidates
	}
	tail := ips[probeCount:]
	probes := make([]probe, probeCount)
	var wg sync.WaitGroup
	for i := 0; i < probeCount; i++ {
		ip := ips[i]
		key := net.JoinHostPort(ip.String(), port)
		probes[i] = probe{idx: i, ip: ip}
		if latency, ok, found := r.latency.get(key, now); found {
			probes[i].latency, probes[i].ok = latency, ok
			continue
		}
		wg.Add(1)
		go func(p *probe, key string) {
			defer wg.Done()
			start := r.now()
			attemptCtx, cancel := context.WithTimeout(ctx, r.timeout)
			conn, err := r.dial(attemptCtx, "tcp", key)
			cancel()
			if err != nil {
				if conn != nil {
					conn.Close()
				}
				if ctx.Err() == nil {
					r.latency.put(key, 0, false, now)
				}
				return
			}
			conn.Close()
			p.ok = true
			p.latency = r.now().Sub(start)
			r.latency.put(key, p.latency, true, now)
		}(&probes[i], key)
	}
	wg.Wait()

	anyOK := false
	for _, p := range probes {
		if p.ok {
			anyOK = true
			break
		}
	}
	if !anyOK {
		return ips
	}
	sort.SliceStable(probes, func(i, j int) bool {
		if probes[i].ok != probes[j].ok {
			return probes[i].ok
		}
		if !probes[i].ok {
			return probes[i].idx < probes[j].idx
		}
		if probes[i].latency != probes[j].latency {
			return probes[i].latency < probes[j].latency
		}
		return probes[i].idx < probes[j].idx
	})
	out := make([]net.IP, 0, len(ips))
	for _, p := range probes {
		if p.ok {
			out = append(out, p.ip)
		}
	}
	out = append(out, tail...)
	for _, p := range probes {
		if !p.ok {
			out = append(out, p.ip)
		}
	}
	return out
}
