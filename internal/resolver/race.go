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
	port    string
	latency *latencyCache
	dial    func(context.Context, string, string) (net.Conn, error)
	now     func() time.Time
}

func (r *racer) Sort(ctx context.Context, ips []net.IP) []net.IP {
	now := r.now()
	type probe struct {
		idx     int
		ip      net.IP
		latency time.Duration
		ok      bool
	}
	probes := make([]probe, len(ips))
	var wg sync.WaitGroup
	for i, ip := range ips {
		probes[i] = probe{idx: i, ip: ip}
		if latency, ok, found := r.latency.get(ip.String(), now); found {
			probes[i].latency, probes[i].ok = latency, ok
			continue
		}
		wg.Add(1)
		go func(p *probe) {
			defer wg.Done()
			start := r.now()
			attemptCtx, cancel := context.WithTimeout(ctx, r.timeout)
			conn, err := r.dial(attemptCtx, "tcp", net.JoinHostPort(p.ip.String(), r.port))
			cancel()
			if err != nil {
				r.latency.put(p.ip.String(), 0, false, now)
				return
			}
			conn.Close()
			p.ok = true
			p.latency = r.now().Sub(start)
			r.latency.put(p.ip.String(), p.latency, true, now)
		}(&probes[i])
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
	out := make([]net.IP, len(ips))
	for i, p := range probes {
		out[i] = p.ip
	}
	return out
}
