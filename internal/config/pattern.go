package config

import (
	"fmt"
	"regexp"
	"strings"
)

// Pattern 是域名表达式：* 表示除点号外任意 0 到多个字符。
type Pattern struct {
	raw      string
	re       *regexp.Regexp
	segments []string
}

// NewPattern 编译域名表达式。
func NewPattern(raw string) (*Pattern, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("域名表达式为空")
	}
	if strings.ContainsAny(raw, " \t") {
		return nil, fmt.Errorf("域名表达式不能包含空白")
	}
	quoted := regexp.QuoteMeta(strings.ToLower(raw))
	expr := "^" + strings.ReplaceAll(quoted, `\*`, "[^.]*") + "$"
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, err
	}
	return &Pattern{raw: raw, re: re, segments: strings.Split(raw, ".")}, nil
}

// Match 判断域名是否匹配。
func (p *Pattern) Match(domain string) bool { return p.re.MatchString(strings.ToLower(domain)) }

// String 返回原始表达式。
func (p *Pattern) String() string { return p.raw }

// Compare 复刻原版排序：段数少者优先，同段数从右往左比较，* 视为最大。
func (p *Pattern) Compare(o *Pattern) int {
	if len(p.segments) != len(o.segments) {
		return len(p.segments) - len(o.segments)
	}
	for i := len(p.segments) - 1; i >= 0; i-- {
		a := strings.ReplaceAll(strings.ToLower(p.segments[i]), "*", "\uffff")
		b := strings.ReplaceAll(strings.ToLower(o.segments[i]), "*", "\uffff")
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
	}
	return 0
}
