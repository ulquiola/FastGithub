package config

import (
	"net"
	"strconv"
	"strings"
	"time"
)

// SNIPattern 返回应发送的 SNI 表达式：tls_sni=false 时不发 SNI。
func (d *Domain) SNIPattern() string {
	if !d.TLSSNI {
		return ""
	}
	if d.TLSSNIPattern == "" {
		return "@domain"
	}
	return d.TLSSNIPattern
}

// ExpandSNI 展开 @domain、@ipaddress、@random（大小写不敏感）。
func ExpandSNI(pattern, domain string, ip net.IP) string {
	out := replaceFold(pattern, "@domain", domain)
	if ip != nil {
		out = replaceFold(out, "@ipaddress", ip.String())
	} else {
		out = replaceFold(out, "@ipaddress", "")
	}
	return replaceFold(out, "@random", strconv.FormatInt(time.Now().UnixMilli(), 10))
}

func replaceFold(s, old, new string) string {
	var b strings.Builder
	for {
		i := indexFold(s, old)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])
		b.WriteString(new)
		s = s[i+len(old):]
	}
}

func indexFold(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if strings.EqualFold(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}
