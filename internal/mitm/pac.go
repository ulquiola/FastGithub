package mitm

import (
	"fmt"
	"strings"
)

// BuildPAC 生成与原版同构的 PAC 文本。
func BuildPAC(patterns []string, proxyHost string) []byte {
	var b strings.Builder
	b.WriteString("function FindProxyForURL(url, host){\n")
	fmt.Fprintf(&b, "    var fastgithub = 'PROXY %s';\n", proxyHost)
	for _, pattern := range patterns {
		fmt.Fprintf(&b, "    if (shExpMatch(host, '%s')) return fastgithub;\n", escapeJS(pattern))
	}
	b.WriteString("    return 'DIRECT';\n}\n")
	return []byte(b.String())
}

func escapeJS(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, `'`, `\'`)
}
