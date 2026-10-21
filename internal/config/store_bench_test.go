package config

import "testing"

func benchmarkStoreLookup(b *testing.B, domains map[string]*Domain, host string) {
	b.Helper()
	cfg := Default()
	cfg.Domains = domains
	store, err := NewStore(cfg)
	if err != nil {
		b.Fatal(err)
	}
	// 预热缓存，测量稳定态下的查询开销。
	store.Lookup(host)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Lookup(host)
	}
}

func BenchmarkStoreLookupHit(b *testing.B) {
	benchmarkStoreLookup(b, map[string]*Domain{
		"github.com":              {},
		"*.github.com":            {},
		"*.githubusercontent.com": {},
	}, "api.github.com")
}

func BenchmarkStoreLookupMiss(b *testing.B) {
	benchmarkStoreLookup(b, map[string]*Domain{"github.com": {}}, "example.org")
}

func BenchmarkPatternMatch(b *testing.B) {
	pattern, err := NewPattern("*.*.azure.com")
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pattern.Match("x.y.azure.com")
	}
}
