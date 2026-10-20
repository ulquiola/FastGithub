package config

import (
	"sort"
	"testing"
)

func TestPatternMatch(t *testing.T) {
	cases := []struct {
		pattern, domain string
		want            bool
	}{
		{"github.com", "github.com", true},
		{"github.com", "api.github.com", false},
		{"*.github.com", "api.github.com", true},
		{"*.github.com", "a.b.github.com", false},
		{"*.*.github.com", "a.b.github.com", true},
		{"GitHub.com", "github.com", true},
		{"github.com", "github.com.evil.com", false},
	}
	for _, c := range cases {
		p, err := NewPattern(c.pattern)
		if err != nil {
			t.Fatalf("NewPattern(%q): %v", c.pattern, err)
		}
		if got := p.Match(c.domain); got != c.want {
			t.Fatalf("%q.Match(%q) = %v，期望 %v", c.pattern, c.domain, got, c.want)
		}
	}
}

func TestPatternRejectsEmpty(t *testing.T) {
	if _, err := NewPattern(""); err == nil {
		t.Fatal("空表达式应报错")
	}
}

func TestPatternOrder(t *testing.T) {
	raw := []string{"*.github.com", "github.com", "*.*.github.com", "api.github.com"}
	patterns := make([]*Pattern, 0, len(raw))
	for _, r := range raw {
		p, err := NewPattern(r)
		if err != nil {
			t.Fatalf("NewPattern(%q): %v", r, err)
		}
		patterns = append(patterns, p)
	}
	sort.SliceStable(patterns, func(i, j int) bool {
		return patterns[i].Compare(patterns[j]) < 0
	})
	want := []string{"github.com", "api.github.com", "*.github.com", "*.*.github.com"}
	for i, w := range want {
		if patterns[i].String() != w {
			t.Fatalf("排序结果[%d] = %q，期望 %q（全量: %v）", i, patterns[i].String(), w, toStrings(patterns))
		}
	}
}

func toStrings(ps []*Pattern) []string {
	out := make([]string, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.String())
	}
	return out
}
