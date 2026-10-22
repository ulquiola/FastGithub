package resolver

import (
	"context"
	"net"

	"fastgithub-go/internal/config"
)

// LookupSource 优先用指定的 DoH 源解析域名；该源失败时回退到全局解析链。
// source 为空时等同 Lookup。
func (r *Resolver) LookupSource(ctx context.Context, domain, source string) ([]net.IP, error) {
	if source == "" {
		return r.Lookup(ctx, domain)
	}
	ips, err := r.customResolver(source).Lookup(ctx, domain)
	if err != nil {
		// 自定义源不可用时保证域名仍能解析；全局源可能返回污染结果，
		// 但比直接失败更符合“可用优先”。
		return r.Lookup(ctx, domain)
	}
	return ips, nil
}

// DiagnoseSource 用指定的 DoH 源诊断域名；source 为空时等同 Diagnose。
// 与 LookupSource 不同，诊断不会回退，方便看清指定源的真实结果。
func (r *Resolver) DiagnoseSource(ctx context.Context, domain, source string) (Diagnosis, error) {
	if source == "" {
		return r.Diagnose(ctx, domain)
	}
	return r.customResolver(source).Diagnose(ctx, domain)
}

// customResolver 为每个 DoH 源维护独立解析器：缓存、竞速与健康状态互不干扰。
func (r *Resolver) customResolver(source string) *Resolver {
	r.customMu.Lock()
	defer r.customMu.Unlock()
	if r.custom == nil {
		r.custom = map[string]*Resolver{}
	}
	if sub, ok := r.custom[source]; ok {
		return sub
	}
	sub := New(config.DNS{DoH: []string{source}, Timeout: config.Duration(r.timeout)})
	// 只使用指定的 DoH 源；失败回退由 LookupSource 负责，避免这里又走一遍明文 DNS。
	sub.fallback = nil
	sub.queryFn = r.queryFn
	sub.now = r.now
	sub.racer.dial = r.racer.dial
	r.custom[source] = sub
	return sub
}
