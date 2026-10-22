package resolver

import (
	"context"
	"fmt"
	"net"
	"time"
)

// Diagnosis 是一次域名诊断的结果。
type Diagnosis struct {
	Host       string
	Candidates []Candidate
	Duration   time.Duration
}

// Diagnose 强制重新解析域名并探测候选 IP 的 TCP 443 延迟。
// 与 Lookup 不同，它不读取也不写入正/负缓存，结果反映当前网络状态。
func (r *Resolver) Diagnose(ctx context.Context, domain string) (Diagnosis, error) {
	domain = normalizeDomain(domain)
	if domain == "" {
		return Diagnosis{}, fmt.Errorf("域名不能为空")
	}
	start := r.now()
	var ips []net.IP
	if ip := net.ParseIP(domain); ip != nil {
		ips = []net.IP{ip}
	} else {
		var err error
		ips, _, err = r.query(ctx, domain)
		if err != nil {
			return Diagnosis{Host: domain, Duration: r.now().Sub(start)}, err
		}
	}
	candidates := r.racer.ProbeFresh(ctx, ips)
	sortCandidates(candidates)
	return Diagnosis{Host: domain, Candidates: candidates, Duration: r.now().Sub(start)}, nil
}
