package config

import (
	"sort"
	"sync"
)

// maxCacheEntries 限制缓存条目数：常驻代理按连接查询域名，唯一域名可能被泛洪。
// 超过上限时整体重置，重置后的语义与新建 Store 一致（未命中会重新计算）。
const maxCacheEntries = 4096

// Store 提供按优先级匹配的域名规则查询，并缓存结果。
type Store struct {
	mu       sync.Mutex
	cfg      *Config
	patterns []*Pattern
	byKey    map[*Pattern]*Domain
	cache    map[string]*Domain
}

// NewStore 校验配置、编译表达式并排序。
func NewStore(cfg *Config) (*Store, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	patterns := make([]*Pattern, 0, len(cfg.Domains))
	byKey := make(map[*Pattern]*Domain, len(cfg.Domains))
	for name, d := range cfg.Domains {
		p, err := NewPattern(name)
		if err != nil {
			return nil, err
		}
		d.pattern = p
		patterns = append(patterns, p)
		byKey[p] = d
	}
	sort.SliceStable(patterns, func(i, j int) bool {
		return patterns[i].Compare(patterns[j]) < 0
	})
	return &Store{
		cfg:      cfg,
		patterns: patterns,
		byKey:    byKey,
		cache:    map[string]*Domain{},
	}, nil
}

// Lookup 返回命中的域名规则；未命中返回 nil,false。
func (s *Store) Lookup(domain string) (*Domain, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d, ok := s.cache[domain]; ok {
		return d, d != nil
	}
	var found *Domain
	for _, p := range s.patterns {
		if p.Match(domain) {
			found = s.byKey[p]
			break
		}
	}
	if len(s.cache) >= maxCacheEntries {
		s.cache = map[string]*Domain{}
	}
	s.cache[domain] = found
	return found, found != nil
}

// Patterns 返回按优先级排序的表达式文本。
func (s *Store) Patterns() []string {
	out := make([]string, 0, len(s.patterns))
	for _, p := range s.patterns {
		out = append(out, p.String())
	}
	return out
}

// Config 返回底层配置（只读）。
func (s *Store) Config() *Config { return s.cfg }
