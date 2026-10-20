package config

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

// Kind 表示 TOML 节点类型。
type Kind int

const (
	KindTable Kind = iota
	KindArray
	KindString
	KindInt
	KindBool
)

// Node 是 TOML 子集的语法树节点，带行号用于错误定位。
type Node struct {
	Kind  Kind
	Line  int
	Str   string
	Int   int64
	Bool  bool
	Arr   []*Node
	Table map[string]*Node
}

func newTable(line int) *Node {
	return &Node{Kind: KindTable, Line: line, Table: map[string]*Node{}}
}

// Parse 解析 TOML 子集：表头、键值、字符串、整数、布尔、数组与 # 注释。
func Parse(data []byte) (*Node, error) {
	data = bytes.TrimPrefix(data, []byte("\xEF\xBB\xBF"))
	root := newTable(1)
	p := &parser{src: data, line: 1, table: root, root: root}
	if err := p.run(); err != nil {
		return nil, err
	}
	return root, nil
}

type parser struct {
	src   []byte
	line  int
	table *Node
	root  *Node
}

func (p *parser) run() error {
	for {
		raw, line, ok := p.nextLine()
		if !ok {
			return nil
		}
		text := strings.TrimSpace(stripComment(raw))
		if text == "" {
			continue
		}
		if err := p.parseLine(text, line); err != nil {
			return fmt.Errorf("toml: 第 %d 行: %w", line, err)
		}
	}
}

func (p *parser) nextLine() (string, int, bool) {
	if len(p.src) == 0 {
		return "", 0, false
	}
	line := p.line
	var raw string
	if i := bytes.IndexByte(p.src, '\n'); i >= 0 {
		raw = string(p.src[:i])
		p.src = p.src[i+1:]
	} else {
		raw = string(p.src)
		p.src = nil
	}
	p.line++
	return strings.TrimSuffix(raw, "\r"), line, true
}

func (p *parser) parseLine(text string, line int) error {
	if text[0] == '[' {
		if strings.HasPrefix(text, "[[") {
			return fmt.Errorf("不支持表数组 [[...]]")
		}
		if !strings.HasSuffix(text, "]") {
			return fmt.Errorf("表头缺少 ']'")
		}
		path, err := parseKeyPath(strings.TrimSpace(text[1 : len(text)-1]))
		if err != nil {
			return err
		}
		table, err := p.walk(path, line)
		if err != nil {
			return err
		}
		p.table = table
		return nil
	}
	eq := indexUnquoted(text, '=')
	if eq < 0 {
		return fmt.Errorf("缺少 '='")
	}
	path, err := parseKeyPath(strings.TrimSpace(text[:eq]))
	if err != nil {
		return err
	}
	val, err := parseValue(strings.TrimSpace(text[eq+1:]), line)
	if err != nil {
		return err
	}
	return p.set(p.table, path, val)
}

func (p *parser) walk(path []string, line int) (*Node, error) {
	t := p.root
	for _, seg := range path {
		next, ok := t.Table[seg]
		if !ok {
			next = newTable(line)
			t.Table[seg] = next
		}
		if next.Kind != KindTable {
			return nil, fmt.Errorf("键 %q 已被定义为值，不能作为表", seg)
		}
		t = next
	}
	return t, nil
}

func (p *parser) set(t *Node, path []string, val *Node) error {
	cur := t
	for _, seg := range path[:len(path)-1] {
		next, ok := cur.Table[seg]
		if !ok {
			next = newTable(val.Line)
			cur.Table[seg] = next
		}
		if next.Kind != KindTable {
			return fmt.Errorf("键 %q 已被定义为值，不能作为表", seg)
		}
		cur = next
	}
	last := path[len(path)-1]
	if _, exists := cur.Table[last]; exists {
		return fmt.Errorf("重复的键 %q", last)
	}
	cur.Table[last] = val
	return nil
}

func parseKeyPath(key string) ([]string, error) {
	var parts []string
	i := 0
	for i < len(key) {
		for i < len(key) && (key[i] == ' ' || key[i] == '\t') {
			i++
		}
		if i >= len(key) {
			break
		}
		if key[i] == '"' {
			str, rest, err := parseQuoted(key[i:])
			if err != nil {
				return nil, err
			}
			parts = append(parts, str)
			i = len(key) - len(rest)
		} else {
			j := i
			for j < len(key) && key[j] != '.' && key[j] != ' ' && key[j] != '\t' {
				j++
			}
			parts = append(parts, key[i:j])
			i = j
		}
		for i < len(key) && (key[i] == ' ' || key[i] == '\t') {
			i++
		}
		if i < len(key) {
			if key[i] != '.' {
				return nil, fmt.Errorf("键名中出现非法字符 %q", string(key[i]))
			}
			i++
			if i >= len(key) {
				return nil, fmt.Errorf("键名以 '.' 结尾")
			}
		}
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("缺少键名")
	}
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("键名不能为空段")
		}
	}
	return parts, nil
}

func parseValue(s string, line int) (*Node, error) {
	if s == "" {
		return nil, fmt.Errorf("缺少值")
	}
	switch s[0] {
	case '"':
		str, rest, err := parseQuoted(s)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(rest) != "" {
			return nil, fmt.Errorf("字符串后存在多余内容 %q", rest)
		}
		return &Node{Kind: KindString, Line: line, Str: str}, nil
	case '[':
		return parseArray(s, line)
	}
	switch s {
	case "true":
		return &Node{Kind: KindBool, Line: line, Bool: true}, nil
	case "false":
		return &Node{Kind: KindBool, Line: line, Bool: false}, nil
	}
	iv, err := strconv.ParseInt(strings.ReplaceAll(s, "_", ""), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("无法识别的值 %q", s)
	}
	return &Node{Kind: KindInt, Line: line, Int: iv}, nil
}

func parseQuoted(s string) (string, string, error) {
	if len(s) == 0 || s[0] != '"' {
		return "", "", fmt.Errorf("缺少引号")
	}
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			return b.String(), s[i+1:], nil
		case '\\':
			if i+1 >= len(s) {
				return "", "", fmt.Errorf("转义不完整")
			}
			switch s[i+1] {
			case '"':
				b.WriteByte('"')
			case '\\':
				b.WriteByte('\\')
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			default:
				return "", "", fmt.Errorf("不支持的转义 \\%c", s[i+1])
			}
			i++
		default:
			b.WriteByte(c)
		}
	}
	return "", "", fmt.Errorf("字符串未闭合")
}

func stripComment(s string) string {
	quoted := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quoted {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				quoted = false
			}
			continue
		}
		switch c {
		case '"':
			quoted = true
		case '#':
			return s[:i]
		}
	}
	return s
}

func indexUnquoted(s string, target byte) int {
	quoted := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quoted {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			continue
		}
		if c == target {
			return i
		}
	}
	return -1
}

func parseArray(s string, line int) (*Node, error) {
	if !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("数组缺少 ']'")
	}
	n := &Node{Kind: KindArray, Line: line}
	inner := strings.TrimSpace(s[1 : len(s)-1])
	if inner == "" {
		return n, nil
	}
	for _, part := range splitTopLevel(inner, ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("数组存在空元素")
		}
		v, err := parseValue(part, line)
		if err != nil {
			return nil, err
		}
		if v.Kind == KindArray {
			return nil, fmt.Errorf("不支持嵌套数组")
		}
		n.Arr = append(n.Arr, v)
	}
	return n, nil
}

func splitTopLevel(s string, sep byte) []string {
	var parts []string
	depth := 0
	quoted := false
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quoted {
			if c == '\\' {
				i++
				continue
			}
			if c == '"' {
				quoted = false
			}
			continue
		}
		switch c {
		case '"':
			quoted = true
		case '[':
			depth++
		case ']':
			depth--
		case sep:
			if depth == 0 {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, s[start:])
}
