package config

import (
	"strings"
	"testing"
)

func TestParseScalars(t *testing.T) {
	root, err := Parse([]byte(`
# 注释
port = 38457
name = "fast\"github"  # 行尾注释
enabled = true
disabled = false
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := root.Table["port"].Int; got != 38457 {
		t.Fatalf("port = %d", got)
	}
	if got := root.Table["name"].Str; got != `fast"github` {
		t.Fatalf("name = %q", got)
	}
	if !root.Table["enabled"].Bool {
		t.Fatal("enabled 应为 true")
	}
	if root.Table["disabled"].Bool {
		t.Fatal("disabled 应为 false")
	}
	if got := root.Table["port"].Line; got != 3 {
		t.Fatalf("port 行号 = %d，期望 3", got)
	}
}

func TestParseRejectsBadValue(t *testing.T) {
	_, err := Parse([]byte("port = not-a-number\n"))
	if err == nil || !strings.Contains(err.Error(), "第 1 行") {
		t.Fatalf("err = %v，期望带行号的错误", err)
	}
}

func TestParseTables(t *testing.T) {
	root, err := Parse([]byte(`
[server]
port = 1

[server.transparent]
https = 443

[domains."*.github.com"]
tls_sni = false
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	server := root.Table["server"].Table
	if server["port"].Int != 1 {
		t.Fatalf("server.port = %d", server["port"].Int)
	}
	trans := server["transparent"].Table
	if trans["https"].Int != 443 {
		t.Fatalf("server.transparent.https = %d", trans["https"].Int)
	}
	domains := root.Table["domains"].Table
	d, ok := domains["*.github.com"]
	if !ok {
		t.Fatalf("缺少域名表，实际键: %v", keysOf(domains))
	}
	if d.Table["tls_sni"].Bool {
		t.Fatal("tls_sni 应为 false")
	}
}

func TestParseDottedKey(t *testing.T) {
	root, err := Parse([]byte("server.proxy_port = 40000\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if root.Table["server"].Table["proxy_port"].Int != 40000 {
		t.Fatal("点号键未展开")
	}
}

func keysOf(m map[string]*Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestParseRejectsBadTableHeader(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{"[[server]]\n", "表数组"},
		{"[server\n", "缺少 ']'"},
		{"a = 1\n[a]\n", "已被定义为值"},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("Parse(%q) err = %v，期望包含 %q", c.src, err, c.want)
		}
	}
}

func TestParseRejectsBadHeaderChars(t *testing.T) {
	bad := []string{
		"[a]]\n",
		"[domains.\"github.com\"]]\n",
		"a] = 1\n",
		"[a]b]\n",
		"[a!b]\n",
	}
	for _, src := range bad {
		_, err := Parse([]byte(src))
		if err == nil || !strings.Contains(err.Error(), "键名包含非法字符") {
			t.Fatalf("Parse(%q) err = %v，期望键名非法字符错误", src, err)
		}
	}
	good := []string{
		"[domains.\"a.b\"]\ntls_sni = false\n",
		"[domains.\"a]b\"]\ntls_sni = false\n",
		"[domains.\"a b\"]\ntls_sni = false\n",
		"[a_b-c9]\nx = 1\n",
		"[ a . b ]\nx = 1\n",
	}
	for _, src := range good {
		if _, err := Parse([]byte(src)); err != nil {
			t.Fatalf("Parse(%q) 不应报错: %v", src, err)
		}
	}
}

func TestParseBadKeyCharKeepsRune(t *testing.T) {
	_, err := Parse([]byte("[dömains]\n"))
	if err == nil || !strings.Contains(err.Error(), `"ö"`) {
		t.Fatalf("err = %v，期望显示完整字符 ö", err)
	}
}

func TestParseRejectsEmptyKeySegment(t *testing.T) {
	for _, key := range []string{"", ".a", "..b", "a..b", "a. .b"} {
		if parts, err := parseKeyPath(key); err == nil {
			t.Fatalf("parseKeyPath(%q) = %#v，期望错误", key, parts)
		}
	}
}

func TestParseStripsBOM(t *testing.T) {
	root, err := Parse([]byte("\xEF\xBB\xBFport = 1\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	n, ok := root.Table["port"]
	if !ok {
		t.Fatalf("带 BOM 的首行未解析，实际键: %v", keysOf(root.Table))
	}
	if n.Int != 1 {
		t.Fatalf("port = %d", n.Int)
	}
}

func TestParseArrays(t *testing.T) {
	root, err := Parse([]byte(`
fallback = ["8.8.8.8:53", "119.29.29.29:53"]
ports = [1, 2, 3]
flags = [true]
empty = []
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	fb := root.Table["fallback"].Arr
	if len(fb) != 2 || fb[1].Str != "119.29.29.29:53" {
		t.Fatalf("fallback = %+v", fb)
	}
	if len(root.Table["ports"].Arr) != 3 || root.Table["ports"].Arr[2].Int != 3 {
		t.Fatalf("ports = %+v", root.Table["ports"].Arr)
	}
	if !root.Table["flags"].Arr[0].Bool {
		t.Fatal("flags[0] 应为 true")
	}
	if len(root.Table["empty"].Arr) != 0 {
		t.Fatal("empty 应为空数组")
	}
}

func TestParseArrayBoundaries(t *testing.T) {
	root, err := Parse([]byte(`a = ["x,y", "p]q"]` + "\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	arr := root.Table["a"].Arr
	if len(arr) != 2 || arr[0].Str != "x,y" || arr[1].Str != "p]q" {
		t.Fatalf("a = %+v", arr)
	}
	cases := []struct {
		src  string
		want string
	}{
		{"a = [[1,2]]\n", "不支持嵌套数组"},
		{"a = [1\n", "数组缺少 ']'"},
		{"a = [1, , 2]\n", "数组存在空元素"},
	}
	for _, c := range cases {
		_, err := Parse([]byte(c.src))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("Parse(%q) err = %v，期望包含 %q", c.src, err, c.want)
		}
	}
}

func TestParseIntegerUnderscores(t *testing.T) {
	root, err := Parse([]byte("n = 1_000_000\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got := root.Table["n"].Int; got != 1000000 {
		t.Fatalf("n = %d", got)
	}
	for _, src := range []string{"n = _1\n", "n = 1_\n", "n = 1__0\n", "n = 1_0_\n"} {
		if _, err := Parse([]byte(src)); err == nil || !strings.Contains(err.Error(), "下划线") {
			t.Fatalf("Parse(%q) err = %v，期望下划线错误", src, err)
		}
	}
}

func TestParseRepeatedTableMerges(t *testing.T) {
	root, err := Parse([]byte("[a]\nx = 1\n[a]\ny = 2\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	a := root.Table["a"].Table
	if a["x"].Int != 1 || a["y"].Int != 2 {
		t.Fatalf("重复表头应合并，实际 %v", keysOf(a))
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		line string // 错误中应包含的行号
	}{
		{"重复键", "a = 1\na = 2\n", "重复的键", "第 2 行"},
		{"表头未闭合", "[a\n", "表头缺少", "第 1 行"},
		{"嵌套数组", "a = [[1]]\n", "不支持嵌套数组", "第 1 行"},
		{"字符串未闭合", `a = "x` + "\n", "字符串未闭合", "第 1 行"},
		{"表数组", "[[a]]\n", "不支持表数组", "第 1 行"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Parse([]byte(c.in))
			if err == nil {
				t.Fatalf("期望错误，实际 nil")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v，期望包含 %q", err, c.want)
			}
			if !strings.Contains(err.Error(), c.line) {
				t.Fatalf("err = %v，期望包含行号 %q", err, c.line)
			}
		})
	}
}
