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

const maxRaceCandidates = 32

// Candidate 是单个候选 IP 的探测结果。
type Candidate struct {
	IP      net.IP        `json:"ip"`
	Latency time.Duration `json:"-"`
	OK      bool          `json:"ok"`
	Probed  bool          `json:"probed"`
}

// Sort 对候选 IP 排序；全部失败时保持原序。
func (r *racer) Sort(ctx context.Context, ips []net.IP) []net.IP {
	candidates := r.Probe(ctx, ips)
	anyOK := false
	for _, c := range candidates {
		if c.OK {
			anyOK = true
			break
		}
	}
	if !anyOK {
		return ips
	}
	sortCandidates(candidates)
	out := make([]net.IP, 0, len(candidates))
	for _, c := range candidates {
		out = append(out, c.IP)
	}
	return out
}

// Probe 探测候选 IP，优先使用时延缓存（常规解析路径）。
func (r *racer) Probe(ctx context.Context, ips []net.IP) []Candidate {
	return r.probe(ctx, ips, true)
}

// ProbeFresh 忽略时延缓存重新探测（诊断路径）。
func (r *racer) ProbeFresh(ctx context.Context, ips []net.IP) []Candidate {
	return r.probe(ctx, ips, false)
}

func (r *racer) probe(ctx context.Context, ips []net.IP, useCache bool) []Candidate {
	now := r.now()
	candidates := make([]Candidate, len(ips))
	for i, ip := range ips {
		candidates[i] = Candidate{IP: ip}
	}
	probeCount := len(candidates)
	if probeCount > maxRaceCandidates {
		probeCount = maxRaceCandidates
	}

	var wg sync.WaitGroup
	for i := 0; i < probeCount; i++ {
		c := &candidates[i]
		if useCache {
			if latency, ok, found := r.latency.get(c.IP.String(), now); found {
				c.Latency, c.OK, c.Probed = latency, ok, true
				continue
			}
		}
		wg.Add(1)
		go func(c *Candidate) {
			defer wg.Done()
			start := r.now()
			attemptCtx, cancel := context.WithTimeout(ctx, r.timeout)
			conn, err := r.dial(attemptCtx, "tcp", net.JoinHostPort(c.IP.String(), r.port))
			cancel()
			c.Probed = true
			if err != nil {
				if conn != nil {
					conn.Close()
				}
				if useCache && ctx.Err() == nil {
					r.latency.put(c.IP.String(), 0, false, now)
				}
				return
			}
			conn.Close()
			c.OK = true
			c.Latency = r.now().Sub(start)
			if useCache {
				r.latency.put(c.IP.String(), c.Latency, true, now)
			}
		}(c)
	}
	wg.Wait()
	return candidates
}

// sortCandidates 把可用项按延迟升序排在前面，失败/未探测项保持原序。
func sortCandidates(candidates []Candidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.OK != b.OK {
			return a.OK
		}
		if !a.OK {
			return false
		}
		return a.Latency < b.Latency
	})
}
