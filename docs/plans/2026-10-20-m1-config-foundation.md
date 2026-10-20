# M1-A 配置内核实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 `fastgithub config validate` 能读懂 TOML 配置、校验语义并预编译域名规则，为后续 DNS/拨号/代理模块提供强类型配置。

**Architecture:** 手写 TOML 子集解析器产出带行号的 `Node` 树 → 反射解码到 `Config`（未知键报错）→ `Validate` 做字段校验 → `NewStore` 预编译域名表达式并排序缓存 → `Loader` 负责路径发现、`conf.d` 合并与指纹计算。

**Tech Stack:** Go 1.22（工具链 1.26.5），仅标准库（`reflect`/`regexp`/`net`/`net/url`/`time`/`flag`），无第三方模块；`CGO_ENABLED=0`。

## Global Constraints

- 模块路径 `fastgithub-go`，`go.mod` 声明 `go 1.22`，**不出现任何 `require`**（本机离线，模块缓存只有 stdlib 与 `golang.org/x/sys`）。
- 所有 `go` 命令前必须设置构建缓存：PowerShell `$env:GOCACHE = "D:\WorkSpace\CodeSpace\FastGithub\.gocache"`；`GOPROXY=off`。
- 本机**没有 `make`**：构建与测试一律直接用 `go` 命令，不依赖 Makefile。
- 所有提交在仓库 `D:\WorkSpace\CodeSpace\FastGithub\fastgithub-go` 内进行，提交信息前缀 `feat(config): ` 或 `test(config): `。
- 面向用户的错误信息用中文；解析错误统一以 `toml: 第 N 行:` 开头。
- 配置字段语义以 `docs/specs/2026-10-20-fastgithub-go-design.md` §6.3 为准，不得改名。
- 每个任务结束时 `go test ./...` 必须全绿，且 `gofmt -l .` 无输出。

### 计划序列（M1 分五份计划，本文件是第一份）

| 计划 | 内容 | 产出 |
|---|---|---|
| **M1-A（本计划）** | 配置内核 + CLI 骨架 | `fastgithub config validate` 可用 |
| M1-B | 证书与信任安装 | `fastgithub install-cert` 可用 |
| M1-C | DNS 解析器 | DoH/回退/缓存/竞速 |
| M1-D | 拨号器与代理 | 38457 代理、隧道、反向代理、PAC 可用 |
| M1-E | 流量统计 + Web UI + 集成测试 | 端到端可跑 |

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `go.mod` | 模块定义，无依赖 |
| `cmd/fastgithub/main.go` | 子命令分发、`version`、`config validate` |
| `internal/app/version.go` | 版本变量（ldflags 注入）与版本串 |
| `internal/config/toml.go` | TOML 子集解析：Node、Parse、表头、键值、数组 |
| `internal/config/decode.go` | 反射解码 + 未知键检查 |
| `internal/config/config.go` | 结构体、默认值、Validate |
| `internal/config/duration.go` | `Duration` 类型与 `UnmarshalText` |
| `internal/config/pattern.go` | 域名表达式：编译、匹配、排序比较 |
| `internal/config/sni.go` | `@domain`/`@ipaddress`/`@random` 展开 |
| `internal/config/destination.go` | `destination` 与 scheme/host 合并 |
| `internal/config/store.go` | 排序查找、缓存、快照 |
| `internal/config/loader.go` | 路径发现、`conf.d` 合并、指纹 |
| `fastgithub.toml`、`conf.d/10-github.toml`、`conf.d/20-google.toml` | 随包配置 |

---

### Task 1: 模块骨架、版本号与 CLI 分发

**Files:**
- Create: `go.mod`
- Create: `internal/app/version.go`
- Test: `internal/app/version_test.go`
- Create: `cmd/fastgithub/main.go`
- Test: `cmd/fastgithub/main_test.go`

**Interfaces:**
- Produces: `app.Version() string`、`app.BuildInfo() string`；`main.run(args []string, stdout, stderr io.Writer) int`

- [ ] **Step 1: 写失败测试**

`internal/app/version_test.go`：

```go
package app

import (
	"strings"
	"testing"
)

func TestBuildInfo(t *testing.T) {
	if Version() == "" {
		t.Fatal("Version() 不能为空")
	}
	if !strings.HasPrefix(BuildInfo(), "fastgithub ") {
		t.Fatalf("BuildInfo() = %q，应以 fastgithub 开头", BuildInfo())
	}
}
```

`cmd/fastgithub/main_test.go`：

```go
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"version"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d，期望 0；stderr=%s", code, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "fastgithub ") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"nope"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d，期望 1", code)
	}
	if !strings.Contains(stderr.String(), "用法") {
		t.Fatalf("stderr 应包含用法说明，实际 %q", stderr.String())
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```
$env:GOCACHE = "D:\WorkSpace\CodeSpace\FastGithub\.gocache"; $env:GOPROXY = "off"
go test ./... 
```

Expected: 编译失败，提示 `no required module provides package fastgithub-go/internal/app` 或 `undefined: run`。

- [ ] **Step 3: 写最小实现**

`go.mod`：

```
module fastgithub-go

go 1.22
```

`internal/app/version.go`：

```go
package app

import "runtime"

var (
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

// Version 返回构建版本，发布时由 -ldflags 注入。
func Version() string { return version }

// BuildInfo 返回版本、提交、构建时间与 Go 运行时信息。
func BuildInfo() string {
	return "fastgithub " + version + " (" + commit + ", " + buildTime + ", " + runtime.Version() + ")"
}
```

`cmd/fastgithub/main.go`：

```go
package main

import (
	"fmt"
	"io"
	"os"

	"fastgithub-go/internal/app"
)

const usage = `用法: fastgithub <命令>

命令:
  config validate [--path 文件]        校验配置
  version                              打印版本信息
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 1
	}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			fmt.Fprint(stderr, usage)
			return 1
		}
		fmt.Fprintln(stdout, app.BuildInfo())
		return 0
	default:
		fmt.Fprint(stderr, usage)
		return 1
	}
}
```

- [ ] **Step 4: 运行测试确认通过**

```
go test ./... -v
```

Expected: `PASS`，`TestBuildInfo`、`TestRunVersion`、`TestRunUnknownCommand` 全绿。

- [ ] **Step 5: 构建并手工验证**

```
go build -o fastgithub.exe ./cmd/fastgithub
.\fastgithub.exe version
```

Expected: 输出形如 `fastgithub dev (none, unknown, go1.26.5)`。

- [ ] **Step 6: 提交**

```
git add go.mod internal/app cmd/fastgithub
git commit -m "feat(config): 建立模块骨架与 version 子命令"
```

---

### Task 2: TOML 解析——注释、键值、字符串、布尔、整数

**Files:**
- Create: `internal/config/toml.go`
- Test: `internal/config/toml_test.go`

**Interfaces:**
- Produces: `config.Parse(data []byte) (*config.Node, error)`；`Node{Kind, Line, Str, Int, Bool, Arr, Table}`；`KindTable/KindArray/KindString/KindInt/KindBool`

- [ ] **Step 1: 写失败测试**

`internal/config/toml_test.go`：

```go
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
```

- [ ] **Step 2: 运行测试确认失败**

```
go test ./internal/config -run TestParse -v
```

Expected: `FAIL`，`undefined: Parse`。

- [ ] **Step 3: 写最小实现**

`internal/config/toml.go`：

```go
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
```

- [ ] **Step 4: 运行测试确认通过**

```
go test ./internal/config -run TestParse -v
```

Expected: `PASS`（`TestParseScalars`、`TestParseRejectsBadValue`）。

- [ ] **Step 5: 提交**

```
git add internal/config/toml.go internal/config/toml_test.go
git commit -m "feat(config): 解析 TOML 标量与注释"
```

---

### Task 3: TOML 解析——表头与点号键

**Files:**
- Modify: `internal/config/toml.go`
- Test: `internal/config/toml_test.go`

**Interfaces:**
- 消费: `Parse`、`Node`、`parseKeyPath`
- Produces: 支持 `[a]`、`[a.b]`、`[domains."*.github.com"]` 表头与 `a.b = 1` 点号键

- [ ] **Step 1: 写失败测试**

追加到 `internal/config/toml_test.go`：

```go
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
```

- [ ] **Step 2: 运行测试确认失败**

```
go test ./internal/config -run "TestParseTables|TestParseDottedKey" -v
```

Expected: `FAIL`，`[server]` 被当作缺少 `'='` 的错误。

- [ ] **Step 3: 写最小实现**

在 `internal/config/toml.go` 的 `parseLine` 开头加入表头分支（替换原函数体第一行）：

```go
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
```

- [ ] **Step 4: 运行测试确认通过**

```
go test ./internal/config -run "TestParse" -v
```

Expected: 全部 `PASS`。

- [ ] **Step 5: 提交**

```
git add internal/config/toml.go internal/config/toml_test.go
git commit -m "feat(config): 支持表头与点号键"
```

---

### Task 4: TOML 解析——数组、重复键、错误定位

**Files:**
- Modify: `internal/config/toml.go`（已含 `parseArray`，本任务只补测试与缺口）
- Test: `internal/config/toml_test.go`

**Interfaces:**
- 消费: `Parse`
- Produces: 字符串/整数/布尔数组；重复键、未闭合表头、嵌套数组、未闭合字符串均返回带行号的错误

- [ ] **Step 1: 写失败测试**

追加到 `internal/config/toml_test.go`：

```go
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

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"重复键", "a = 1\na = 2\n", "重复的键"},
		{"表头未闭合", "[a\n", "表头缺少"},
		{"嵌套数组", "a = [[1]]\n", "不支持嵌套数组"},
		{"字符串未闭合", `a = "x` + "\n", "字符串未闭合"},
		{"表数组", "[[a]]\n", "不支持表数组"},
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
			if !strings.Contains(err.Error(), "第 1 行") {
				t.Fatalf("err = %v，期望包含行号", err)
			}
		})
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```
go test ./internal/config -run "TestParseArrays|TestParseErrors" -v
```

Expected: `FAIL`，数组相关用例报 `无法识别的值 "["`；`重复键`、`表头未闭合`、`嵌套数组`、`表数组` 用例此时可能已通过（`set`/表头分支已实现），以数组用例失败为准。

- [ ] **Step 3: 写最小实现**

在 `internal/config/toml.go` 的 `parseValue` 中补上数组分支（放在 `switch s[0]` 内、`case '"'` 之后）：

```go
	case '[':
		return parseArray(s, line)
```

并在文件末尾追加：

```go
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
```

- [ ] **Step 4: 运行测试确认通过**

```
go test ./internal/config -v
```

Expected: 全部 `PASS`。

- [ ] **Step 5: 提交**

```
git add internal/config/toml_test.go
git commit -m "test(config): 覆盖数组与解析错误"
```

---

### Task 5: 反射解码与未知键检查

**Files:**
- Create: `internal/config/decode.go`
- Test: `internal/config/decode_test.go`

**Interfaces:**
- Produces: `config.Decode(n *Node, out any) error`；结构体字段用 `toml:"name"` 标签映射；未识别键聚合报错

- [ ] **Step 1: 写失败测试**

`internal/config/decode_test.go`：

```go
package config

import (
	"strings"
	"testing"
)

type sampleSub struct {
	On bool `toml:"on"`
}

type sample struct {
	Port  int         `toml:"port"`
	Name  string      `toml:"name"`
	Flags []string    `toml:"flags"`
	Sub   *sampleSub  `toml:"sub"`
	Inner struct {
		Deep int `toml:"deep"`
	} `toml:"inner"`
}

func TestDecode(t *testing.T) {
	root, err := Parse([]byte(`
port = 8080
name = "x"
flags = ["a", "b"]

[sub]
on = true

[inner]
deep = 7
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var s sample
	if err := Decode(root, &s); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if s.Port != 8080 || s.Name != "x" || len(s.Flags) != 2 || s.Flags[1] != "b" {
		t.Fatalf("s = %+v", s)
	}
	if s.Sub == nil || !s.Sub.On {
		t.Fatalf("sub = %+v", s.Sub)
	}
	if s.Inner.Deep != 7 {
		t.Fatalf("inner.deep = %d", s.Inner.Deep)
	}
}

func TestDecodeUnknownKey(t *testing.T) {
	root, err := Parse([]byte("port = 1\nunknown = 2\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var s sample
	err = Decode(root, &s)
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("err = %v，期望包含未知键 unknown", err)
	}
}

func TestDecodeTypeMismatch(t *testing.T) {
	root, err := Parse([]byte(`port = "x"` + "\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var s sample
	if err := Decode(root, &s); err == nil || !strings.Contains(err.Error(), "port") {
		t.Fatalf("err = %v，期望包含 port", err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```
go test ./internal/config -run TestDecode -v
```

Expected: `FAIL`，`undefined: Decode`。

- [ ] **Step 3: 写最小实现**

`internal/config/decode.go`：

```go
package config

import (
	"encoding"
	"fmt"
	"reflect"
	"strings"
)

// Decode 把语法树解码到 out（必须是非空指针）。
// 未识别的键会聚合到同一个错误里。
func Decode(n *Node, out any) error {
	rv := reflect.ValueOf(out)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("Decode 需要非空指针")
	}
	var unknown []string
	if err := decodeInto(n, rv.Elem(), "", &unknown); err != nil {
		return err
	}
	if len(unknown) > 0 {
		return fmt.Errorf("未知配置项: %s", strings.Join(unknown, ", "))
	}
	return nil
}

func decodeInto(n *Node, rv reflect.Value, path string, unknown *[]string) error {
	if !rv.CanSet() {
		return fmt.Errorf("%s: 字段不可写", path)
	}
	if n.Kind == KindString && rv.CanAddr() {
		if u, ok := rv.Addr().Interface().(encoding.TextUnmarshaler); ok {
			if err := u.UnmarshalText([]byte(n.Str)); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			return nil
		}
	}
	switch rv.Kind() {
	case reflect.Ptr:
		if n.Kind != KindTable {
			return typeErr(path, "表")
		}
		if rv.IsNil() {
			rv.Set(reflect.New(rv.Type().Elem()))
		}
		return decodeInto(n, rv.Elem(), path, unknown)
	case reflect.Struct:
		if n.Kind != KindTable {
			return typeErr(path, "表")
		}
		fields := map[string]reflect.Value{}
		for i := 0; i < rv.NumField(); i++ {
			f := rv.Type().Field(i)
			if f.PkgPath != "" {
				continue
			}
			tag := f.Tag.Get("toml")
			if tag == "" || tag == "-" {
				continue
			}
			fields[tag] = rv.Field(i)
		}
		for key, child := range n.Table {
			fv, ok := fields[key]
			if !ok {
				*unknown = append(*unknown, joinPath(path, key))
				continue
			}
			if err := decodeInto(child, fv, joinPath(path, key), unknown); err != nil {
				return err
			}
		}
		return nil
	case reflect.Map:
		if n.Kind != KindTable {
			return typeErr(path, "表")
		}
		if rv.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("%s: 仅支持字符串键", path)
		}
		if rv.IsNil() {
			rv.Set(reflect.MakeMap(rv.Type()))
		}
		for key, child := range n.Table {
			elem := reflect.New(rv.Type().Elem()).Elem()
			if err := decodeInto(child, elem, joinPath(path, key), unknown); err != nil {
				return err
			}
			rv.SetMapIndex(reflect.ValueOf(key), elem)
		}
		return nil
	case reflect.Slice:
		if n.Kind != KindArray {
			return typeErr(path, "数组")
		}
		if rv.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("%s: 仅支持字符串数组", path)
		}
		out := reflect.MakeSlice(rv.Type(), 0, len(n.Arr))
		for i, item := range n.Arr {
			if item.Kind != KindString {
				return typeErr(fmt.Sprintf("%s[%d]", path, i), "字符串")
			}
			out = reflect.Append(out, reflect.ValueOf(item.Str))
		}
		rv.Set(out)
		return nil
	case reflect.String:
		if n.Kind != KindString {
			return typeErr(path, "字符串")
		}
		rv.SetString(n.Str)
		return nil
	case reflect.Bool:
		if n.Kind != KindBool {
			return typeErr(path, "布尔")
		}
		rv.SetBool(n.Bool)
		return nil
	case reflect.Int, reflect.Int64:
		if n.Kind != KindInt {
			return typeErr(path, "整数")
		}
		rv.SetInt(n.Int)
		return nil
	default:
		return fmt.Errorf("%s: 不支持的类型 %s", path, rv.Type())
	}
}

func typeErr(path, want string) error {
	if path == "" {
		return fmt.Errorf("期望%s", want)
	}
	return fmt.Errorf("%s: 期望%s", path, want)
}

func joinPath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}
```

- [ ] **Step 4: 运行测试确认通过**

```
go test ./internal/config -run TestDecode -v
```

Expected: `PASS`。

- [ ] **Step 5: 提交**

```
git add internal/config/decode.go internal/config/decode_test.go
git commit -m "feat(config): 反射解码并拒绝未知配置项"
```

---

### Task 6: 配置结构体、默认值、Duration 与校验

> **执行说明：** 本任务与 Task 7 同属一个编译单元（`Validate` 调用 `NewPattern`，而 Task 7 的 `sni.go`/`destination.go` 引用本任务的 `Domain` 类型）。实施时必须把 Task 6 与 Task 7 **合并为一次实现与一次提交序列**，两个任务共用一份 brief；评审也按一份 diff 进行。

**Files:**
- Create: `internal/config/config.go`
- Create: `internal/config/duration.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `config.Config`/`Server`/`Transparent`/`DNS`/`Intercept`/`Log`/`Domain`/`Response` 类型；`config.Default() *Config`；`(*Config).Validate() error`；`config.Duration`（含 `Std()`）

- [ ] **Step 1: 写失败测试**

`internal/config/config_test.go`：

```go
package config

import (
	"strings"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.Server.ProxyPort != 38457 || c.Server.UIPort != 38470 {
		t.Fatalf("端口默认值错误: %+v", c.Server)
	}
	if c.Server.Transparent.HTTPS != 443 || c.Server.Transparent.Git != 9418 {
		t.Fatalf("透明端口默认值错误: %+v", c.Server.Transparent)
	}
	if len(c.DNS.Fallback) != 3 || len(c.DNS.DoH) != 2 {
		t.Fatalf("DNS 默认值错误: %+v", c.DNS)
	}
	if c.DNS.Timeout.Std() != 4*time.Second {
		t.Fatalf("DNS 超时 = %v", c.DNS.Timeout.Std())
	}
	if c.Intercept.Mode != "auto" {
		t.Fatalf("intercept.mode = %q", c.Intercept.Mode)
	}
	if c.Log.Level != "info" || c.Log.File != "logs/fastgithub.log" {
		t.Fatalf("log 默认值错误: %+v", c.Log)
	}
}

func TestDecodeIntoConfig(t *testing.T) {
	c := Default()
	root, err := Parse([]byte(`
[server]
proxy_port = 40000

[dns]
timeout = "1500ms"

[domains."github.com"]
tls_sni = false

[domains."*.githubusercontent.com"]
tls_ignore_name_mismatch = true
`))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := Decode(root, c); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if c.Server.ProxyPort != 40000 || c.Server.UIPort != 38470 {
		t.Fatalf("server = %+v", c.Server)
	}
	if c.DNS.Timeout.Std() != 1500*time.Millisecond {
		t.Fatalf("timeout = %v", c.DNS.Timeout.Std())
	}
	if len(c.Domains) != 2 || c.Domains["github.com"].TLSSNI {
		t.Fatalf("domains = %+v", c.Domains)
	}
}

func TestValidateErrors(t *testing.T) {
	c := Default()
	c.Server.ProxyPort = 0
	c.Server.UIPort = 0
	c.Intercept.Mode = "maybe"
	c.Log.Level = "verbose"
	c.DNS.DoH = []string{"http://insecure/dns-query"}
	c.DNS.Fallback = []string{"8.8.8.8"}
	c.Domains = map[string]*Domain{"": {}}
	err := c.Validate()
	if err == nil {
		t.Fatal("期望校验失败")
	}
	for _, want := range []string{"server.proxy_port", "intercept.mode", "log.level", "dns.doh", "dns.fallback", "域名规则"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("err = %v，缺少 %q", err, want)
		}
	}
}

func TestValidateOK(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("默认配置应通过校验: %v", err)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```
go test ./internal/config -run "TestDefault|TestDecodeIntoConfig|TestValidate" -v
```

Expected: `FAIL`，`undefined: Default`。

- [ ] **Step 3: 写最小实现**

`internal/config/duration.go`：

```go
package config

import "time"

// Duration 是 TOML 中以字符串书写的时长，如 "4s"、"1500ms"。
type Duration time.Duration

// UnmarshalText 实现 encoding.TextUnmarshaler。
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// Std 返回标准库时长。
func (d Duration) Std() time.Duration { return time.Duration(d) }

// String 返回 "4s" 这类文本形式。
func (d Duration) String() string { return time.Duration(d).String() }
```

`internal/config/config.go`：

```go
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Config 对应 fastgithub.toml 的根表。
type Config struct {
	Server    Server             `toml:"server"`
	DNS       DNS                `toml:"dns"`
	Intercept Intercept          `toml:"intercept"`
	Log       Log                `toml:"log"`
	Domains   map[string]*Domain `toml:"domains"`
}

// Server 是监听相关配置。
type Server struct {
	ProxyPort   int         `toml:"proxy_port"`
	UIPort      int         `toml:"ui_port"`
	Singleton   bool        `toml:"singleton"`
	Transparent Transparent `toml:"transparent"`
}

// Transparent 是 Windows 透明入口的偏好端口。
type Transparent struct {
	HTTPS int `toml:"https"`
	HTTP  int `toml:"http"`
	SSH   int `toml:"ssh"`
	Git   int `toml:"git"`
}

// DNS 是域名解析配置。
type DNS struct {
	DoH      []string `toml:"doh"`
	Fallback []string `toml:"fallback"`
	Timeout  Duration `toml:"timeout"`
}

// Intercept 是平台劫持配置。
type Intercept struct {
	Mode      string `toml:"mode"`
	DriverDir string `toml:"driver_dir"`
}

// Log 是日志配置。
type Log struct {
	Level string `toml:"level"`
	File  string `toml:"file"`
}

// Domain 是单个域名规则的配置。
type Domain struct {
	TLSSNI                bool      `toml:"tls_sni"`
	TLSSNIPattern         string    `toml:"tls_sni_pattern"`
	TLSIgnoreNameMismatch bool      `toml:"tls_ignore_name_mismatch"`
	Timeout               Duration  `toml:"timeout"`
	IP                    string    `toml:"ip"`
	Destination           string    `toml:"destination"`
	Response              *Response `toml:"response"`

	pattern *Pattern
	dest    *url.URL
	ipAddr  net.IP
}

// Response 是短路响应的配置。
type Response struct {
	Status      int    `toml:"status"`
	ContentType string `toml:"content_type"`
	Content     string `toml:"content"`
}

// Default 返回与设计文档一致的内置默认值。
func Default() *Config {
	return &Config{
		Server: Server{
			ProxyPort: 38457,
			UIPort:    38470,
			Singleton: true,
			Transparent: Transparent{
				HTTPS: 443,
				HTTP:  80,
				SSH:   22,
				Git:   9418,
			},
		},
		DNS: DNS{
			DoH: []string{
				"https://doh.pub/dns-query",
				"https://dns.alidns.com/dns-query",
			},
			Fallback: []string{
				"8.8.8.8:53",
				"119.29.29.29:53",
				"114.114.114.114:53",
			},
			Timeout: Duration(4 * time.Second),
		},
		Intercept: Intercept{Mode: "auto"},
		Log:       Log{Level: "info", File: "logs/fastgithub.log"},
		Domains:   map[string]*Domain{},
	}
}

// Validate 校验字段取值范围与格式，返回聚合错误。
func (c *Config) Validate() error {
	var errs []string
	add := func(format string, args ...any) {
		errs = append(errs, fmt.Sprintf(format, args...))
	}

	if c.Server.ProxyPort < 1 || c.Server.ProxyPort > 65535 {
		add("server.proxy_port 必须在 1~65535 之间")
	}
	if c.Server.UIPort < 1 || c.Server.UIPort > 65535 {
		add("server.ui_port 必须在 1~65535 之间")
	}
	if c.Server.ProxyPort == c.Server.UIPort {
		add("server.ui_port 不能与 server.proxy_port 相同")
	}

	ports := []struct {
		name string
		port int
	}{
		{"https", c.Server.Transparent.HTTPS},
		{"http", c.Server.Transparent.HTTP},
		{"ssh", c.Server.Transparent.SSH},
		{"git", c.Server.Transparent.Git},
	}
	seen := map[int]string{}
	for _, p := range ports {
		if p.port < 1 || p.port > 65535 {
			add("server.transparent.%s 必须在 1~65535 之间", p.name)
			continue
		}
		if other, ok := seen[p.port]; ok {
			add("server.transparent.%s 与 %s 端口重复", p.name, other)
		}
		seen[p.port] = p.name
	}

	if c.Intercept.Mode != "auto" && c.Intercept.Mode != "off" {
		add("intercept.mode 只能是 auto 或 off")
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		add("log.level 只能是 debug/info/warn/error")
	}

	if len(c.DNS.DoH) == 0 && len(c.DNS.Fallback) == 0 {
		add("dns.doh 与 dns.fallback 不能同时为空")
	}
	for i, raw := range c.DNS.DoH {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			add("dns.doh[%d] 必须是 https URL", i)
		}
	}
	for i, raw := range c.DNS.Fallback {
		host, port, err := net.SplitHostPort(raw)
		if err != nil || host == "" || port == "" {
			add("dns.fallback[%d] 必须是 host:port", i)
		}
	}
	if c.DNS.Timeout.Std() <= 0 {
		add("dns.timeout 必须大于 0")
	}

	names := make([]string, 0, len(c.Domains))
	for name := range c.Domains {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		d := c.Domains[name]
		if name == "" {
			add("域名规则不能为空")
			continue
		}
		if _, err := NewPattern(name); err != nil {
			add("域名规则 %q: %v", name, err)
			continue
		}
		if d == nil {
			add("域名规则 %q 的配置为空", name)
			continue
		}
		if d.IP != "" && net.ParseIP(d.IP) == nil {
			add("域名规则 %q 的 ip 无法解析: %s", name, d.IP)
		}
		if d.Destination != "" {
			if _, err := url.Parse(d.Destination); err != nil {
				add("域名规则 %q 的 destination 无法解析: %v", name, err)
			}
		}
		if d.Response != nil {
			if d.Response.Status < 100 || d.Response.Status > 599 {
				add("域名规则 %q 的 response.status 必须在 100~599 之间", name)
			}
			if d.Response.Content != "" && d.Response.ContentType == "" {
				add("域名规则 %q 有 response.content 时必须给出 response.content_type", name)
			}
		}
	}

	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}
```

- [ ] **Step 4: 运行测试确认通过**

```
go test ./internal/config -run "TestDefault|TestDecodeIntoConfig|TestValidate" -v
```

Expected: `PASS`。

> 若报 `undefined: NewPattern`，先完成 Task 7 再回来；两个任务同属一个编译单元，允许把 Task 7 的 `pattern.go` 一起提交。

- [ ] **Step 5: 提交**

```
git add internal/config/config.go internal/config/duration.go internal/config/config_test.go
git commit -m "feat(config): 配置结构体、默认值与校验"
```

---

### Task 7: 域名表达式、SNI 表达式、Destination 合并

**Files:**
- Create: `internal/config/pattern.go`
- Create: `internal/config/sni.go`
- Create: `internal/config/destination.go`
- Test: `internal/config/pattern_test.go`
- Test: `internal/config/sni_test.go`
- Test: `internal/config/destination_test.go`

**Interfaces:**
- Produces: `NewPattern(string) (*Pattern, error)`、`(*Pattern).Match(string) bool`、`(*Pattern).String() string`、`(*Pattern).Compare(*Pattern) int`；`ExpandSNI(pattern, domain string, ip net.IP) string`、`(*Domain).SNIPattern() string`；`JoinDestination(scheme, host, destination string) (*url.URL, error)`、`(*Domain).DestinationPrefix(scheme, host string) (*url.URL, error)`

- [ ] **Step 1: 写失败测试**

`internal/config/pattern_test.go`：

```go
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
```

`internal/config/sni_test.go`：

```go
package config

import (
	"net"
	"strings"
	"testing"
)

func TestExpandSNI(t *testing.T) {
	if got := ExpandSNI("@domain", "github.com", nil); got != "github.com" {
		t.Fatalf("got %q", got)
	}
	if got := ExpandSNI("@IPADDRESS", "github.com", net.ParseIP("1.2.3.4")); got != "1.2.3.4" {
		t.Fatalf("got %q", got)
	}
	got := ExpandSNI("@random", "", nil)
	if got == "" || got == "@random" || strings.Contains(got, "@") {
		t.Fatalf("random 未展开: %q", got)
	}
}

func TestDomainSNIPattern(t *testing.T) {
	if got := (&Domain{TLSSNI: false, TLSSNIPattern: "@random"}).SNIPattern(); got != "" {
		t.Fatalf("tls_sni=false 应返回空，实际 %q", got)
	}
	if got := (&Domain{TLSSNI: true}).SNIPattern(); got != "@domain" {
		t.Fatalf("默认应为 @domain，实际 %q", got)
	}
	if got := (&Domain{TLSSNI: true, TLSSNIPattern: "@ipaddress"}).SNIPattern(); got != "@ipaddress" {
		t.Fatalf("got %q", got)
	}
}
```

`internal/config/destination_test.go`：

```go
package config

import "testing"

func TestJoinDestination(t *testing.T) {
	cases := []struct {
		scheme, host, dest, want string
	}{
		{"https", "github.com", "", "https://github.com/"},
		{"https", "fonts.googleapis.com", "https://fonts.geekzu.org/", "https://fonts.geekzu.org/"},
		{"https", "github.com", "/mirror/", "https://github.com/mirror/"},
		{"http", "github.com", "", "http://github.com/"},
	}
	for _, c := range cases {
		u, err := JoinDestination(c.scheme, c.host, c.dest)
		if err != nil {
			t.Fatalf("JoinDestination(%q,%q,%q): %v", c.scheme, c.host, c.dest, err)
		}
		if u.String() != c.want {
			t.Fatalf("got %q，期望 %q", u.String(), c.want)
		}
	}
}

func TestJoinDestinationInvalid(t *testing.T) {
	if _, err := JoinDestination("https", "github.com", "://bad"); err == nil {
		t.Fatal("非法 destination 应报错")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```
go test ./internal/config -run "TestPattern|TestExpandSNI|TestDomainSNIPattern|TestJoinDestination" -v
```

Expected: `FAIL`，`undefined: NewPattern`。

- [ ] **Step 3: 写最小实现**

`internal/config/pattern.go`：

```go
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
```

`internal/config/sni.go`：

```go
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
```

`internal/config/destination.go`：

```go
package config

import (
	"net/url"
)

// JoinDestination 把请求的 scheme/host 与配置的 destination 合并为上游前缀。
// 语义等价 .NET 的 new Uri(baseUri, destination)。
func JoinDestination(scheme, host, destination string) (*url.URL, error) {
	base, err := url.Parse(scheme + "://" + host + "/")
	if err != nil {
		return nil, err
	}
	if destination == "" {
		return base, nil
	}
	ref, err := url.Parse(destination)
	if err != nil {
		return nil, err
	}
	if ref.IsAbs() {
		return ref, nil
	}
	return base.ResolveReference(ref), nil
}

// DestinationPrefix 是 JoinDestination 的方法形式。
func (d *Domain) DestinationPrefix(scheme, host string) (*url.URL, error) {
	return JoinDestination(scheme, host, d.Destination)
}
```

- [ ] **Step 4: 运行测试确认通过**

```
go test ./internal/config -v
```

Expected: 全部 `PASS`（包括 Task 6 的校验测试）。

- [ ] **Step 5: 提交**

```
git add internal/config/pattern.go internal/config/sni.go internal/config/destination.go internal/config/pattern_test.go internal/config/sni_test.go internal/config/destination_test.go
git commit -m "feat(config): 域名表达式、SNI 展开与 destination 合并"
```

---

### Task 8: Store（排序查找 + 缓存）与 Loader（发现 / 合并 / 指纹）

**Files:**
- Create: `internal/config/store.go`
- Create: `internal/config/loader.go`
- Test: `internal/config/store_test.go`
- Test: `internal/config/loader_test.go`

**Interfaces:**
- Consumes: `Default`、`Decode`、`Parse`、`NewPattern`、`Validate`
- Produces: `NewStore(*Config) (*Store, error)`、`(*Store).Lookup(string) (*Domain, bool)`、`(*Store).Patterns() []string`、`(*Store).Config() *Config`；`Load(mainPath, confDir string) (*Config, error)`、`Discover(explicit string) (mainPath, confDir string, err error)`、`Fingerprint(mainPath, confDir string) (string, error)`

- [ ] **Step 1: 写失败测试**

`internal/config/store_test.go`：

```go
package config

import "testing"

func TestStoreLookupPrefersExact(t *testing.T) {
	c := Default()
	c.Domains = map[string]*Domain{
		"*.github.com": {TLSIgnoreNameMismatch: true},
		"github.com":   {TLSSNI: true},
	}
	st, err := NewStore(c)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	d, ok := st.Lookup("github.com")
	if !ok || !d.TLSSNI {
		t.Fatalf("github.com 应命中精确规则，got=%+v ok=%v", d, ok)
	}
	d, ok = st.Lookup("api.github.com")
	if !ok || !d.TLSIgnoreNameMismatch {
		t.Fatalf("api.github.com 应命中通配规则，got=%+v ok=%v", d, ok)
	}
	if _, ok := st.Lookup("example.com"); ok {
		t.Fatal("example.com 不应命中")
	}
	if got := st.Patterns(); len(got) != 2 {
		t.Fatalf("Patterns() = %v", got)
	}
}

func TestStoreCachesNegative(t *testing.T) {
	c := Default()
	c.Domains = map[string]*Domain{"github.com": {}}
	st, err := NewStore(c)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, ok := st.Lookup("nope.example"); ok {
			t.Fatal("不应命中")
		}
	}
}

func TestStoreRejectsInvalidConfig(t *testing.T) {
	c := Default()
	c.Server.ProxyPort = 0
	if _, err := NewStore(c); err == nil {
		t.Fatal("非法配置应被拒绝")
	}
}
```

`internal/config/loader_test.go`：

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func TestLoadMergesConfD(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "fastgithub.toml"), "[server]\nproxy_port = 40000\n")
	write(t, filepath.Join(dir, "conf.d", "10-a.toml"), "[domains.\"a.com\"]\ntls_sni = true\n")
	write(t, filepath.Join(dir, "conf.d", "20-b.toml"), "[server]\nui_port = 40001\n")

	cfg, err := Load(filepath.Join(dir, "fastgithub.toml"), filepath.Join(dir, "conf.d"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server.ProxyPort != 40000 || cfg.Server.UIPort != 40001 {
		t.Fatalf("server = %+v", cfg.Server)
	}
	if _, ok := cfg.Domains["a.com"]; !ok {
		t.Fatalf("domains = %v", cfg.Domains)
	}
	if len(cfg.DNS.Fallback) != 3 {
		t.Fatalf("默认值未生效: %v", cfg.DNS.Fallback)
	}
}

func TestLoadMissingMainFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := Load(filepath.Join(dir, "nope.toml"), filepath.Join(dir, "conf.d")); err == nil {
		t.Fatal("主配置缺失应报错")
	}
}

func TestLoadReportsFileAndLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fastgithub.toml")
	write(t, path, "[server]\nproxy_port = oops\n")
	_, err := Load(path, "")
	if err == nil {
		t.Fatal("期望报错")
	}
	if got := err.Error(); !contains(got, "fastgithub.toml") || !contains(got, "第 2 行") {
		t.Fatalf("err = %v，期望包含文件名与行号", got)
	}
}

func TestFingerprintChanges(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "fastgithub.toml")
	write(t, main, "[server]\nproxy_port = 40000\n")
	fp1, err := Fingerprint(main, "")
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if fp1 == "" {
		t.Fatal("指纹不能为空")
	}
	write(t, main, "[server]\nproxy_port = 40001\n")
	fp2, err := Fingerprint(main, "")
	if err != nil {
		t.Fatalf("Fingerprint: %v", err)
	}
	if fp1 == fp2 {
		t.Fatal("内容变化后指纹应变化")
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0) }

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 2: 运行测试确认失败**

```
go test ./internal/config -run "TestStore|TestLoad|TestFingerprint" -v
```

Expected: `FAIL`，`undefined: NewStore`。

- [ ] **Step 3: 写最小实现**

`internal/config/store.go`：

```go
package config

import (
	"sort"
	"sync"
)

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
```

`internal/config/loader.go`：

```go
package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Discover 定位主配置与 conf.d 目录：显式路径优先，其次当前目录，最后用户配置目录。
func Discover(explicit string) (mainPath, confDir string, err error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", "", err
		}
		return abs, filepath.Join(filepath.Dir(abs), "conf.d"), nil
	}
	if _, statErr := os.Stat("fastgithub.toml"); statErr == nil {
		abs, err := filepath.Abs("fastgithub.toml")
		if err != nil {
			return "", "", err
		}
		return abs, filepath.Join(filepath.Dir(abs), "conf.d"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", "", fmt.Errorf("无法定位配置目录: %w", err)
	}
	dir := filepath.Join(base, "fastgithub")
	return filepath.Join(dir, "fastgithub.toml"), filepath.Join(dir, "conf.d"), nil
}

// Load 读取主配置与 conf.d 下的 TOML 片段（按文件名升序合并）。
func Load(mainPath, confDir string) (*Config, error) {
	cfg := Default()
	if err := loadFile(cfg, mainPath, true); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(confDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("读取 %s 失败: %w", confDir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := loadFile(cfg, filepath.Join(confDir, name), false); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func loadFile(cfg *Config, path string, required bool) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if !required && os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 %s 失败: %w", filepath.Base(path), err)
	}
	root, err := Parse(data)
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if err := Decode(root, cfg); err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return nil
}

// Fingerprint 返回主配置与 conf.d 全部文件内容的摘要，用于热重载检测。
func Fingerprint(mainPath, confDir string) (string, error) {
	h := sha256.New()
	paths := []string{mainPath}
	entries, err := os.ReadDir(confDir)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
			paths = append(paths, filepath.Join(confDir, e.Name()))
		}
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return "", err
		}
		h.Write([]byte(p))
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
```

- [ ] **Step 4: 运行测试确认通过**

```
go test ./internal/config -v
```

Expected: 全部 `PASS`。

- [ ] **Step 5: 提交**

```
git add internal/config/store.go internal/config/loader.go internal/config/store_test.go internal/config/loader_test.go
git commit -m "feat(config): Store 匹配缓存与 Loader 合并加载"
```

---

### Task 9: 随包配置片段与 golden 测试

**Files:**
- Create: `fastgithub.toml`
- Create: `conf.d/10-github.toml`
- Create: `conf.d/20-google.toml`
- Test: `internal/config/shipped_test.go`

**Interfaces:**
- Consumes: `Load`、`NewStore`、`(*Store).Lookup`
- Produces: 可被 CLI 校验、被后续模块直接加载的默认配置

- [ ] **Step 1: 写失败测试**

`internal/config/shipped_test.go`：

```go
package config

import (
	"path/filepath"
	"testing"
)

func TestShippedConfig(t *testing.T) {
	root := filepath.Join("..", "..")
	cfg, err := Load(filepath.Join(root, "fastgithub.toml"), filepath.Join(root, "conf.d"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	st, err := NewStore(cfg)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	d, ok := st.Lookup("github.com")
	if !ok {
		t.Fatal("github.com 未命中")
	}
	if d.TLSSNI {
		t.Fatal("github.com 不应发送 SNI")
	}

	d, ok = st.Lookup("raw.githubusercontent.com")
	if !ok || !d.TLSIgnoreNameMismatch {
		t.Fatalf("raw.githubusercontent.com 应忽略证书名不匹配，got=%+v", d)
	}

	d, ok = st.Lookup("fonts.googleapis.com")
	if !ok || d.Destination != "https://fonts.geekzu.org/" {
		t.Fatalf("fonts.googleapis.com 配置错误: %+v", d)
	}

	if _, ok := st.Lookup("example.com"); ok {
		t.Fatal("example.com 不应命中")
	}
	if cfg.Server.ProxyPort != 38457 || cfg.Server.UIPort != 38470 {
		t.Fatalf("端口不符: %+v", cfg.Server)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```
go test ./internal/config -run TestShippedConfig -v
```

Expected: `FAIL`，`读取 fastgithub.toml 失败`。

- [ ] **Step 3: 写配置文件**

`fastgithub.toml`：

```toml
# FastGithub 配置。字段语义见 docs/specs/2026-10-20-fastgithub-go-design.md §6.3。

[server]
proxy_port = 38457
ui_port    = 38470
singleton  = true

[server.transparent]
https = 443
http  = 80
ssh   = 22
git   = 9418

[dns]
doh      = ["https://doh.pub/dns-query", "https://dns.alidns.com/dns-query"]
fallback = ["8.8.8.8:53", "119.29.29.29:53", "114.114.114.114:53"]
timeout  = "4s"

[intercept]
mode       = "auto"
driver_dir = ""

[log]
level = "info"
file  = "logs/fastgithub.log"
```

`conf.d/10-github.toml`：

```toml
# 转写自原版 appsettings.github.json

[domains."github.com"]
tls_sni = false

[domains."api.github.com"]
tls_sni = false

[domains."githubstatus.com"]
tls_sni = false

[domains."gist.github.com"]
tls_ignore_name_mismatch = true

[domains."vscode-auth.github.com"]
tls_sni = true

[domains."*.github.com"]
tls_ignore_name_mismatch = true

[domains."*.github.io"]
tls_ignore_name_mismatch = true

[domains."*.githubapp.com"]
tls_ignore_name_mismatch = true

[domains."*.githubassets.com"]
tls_ignore_name_mismatch = true

[domains."*.githubusercontent.com"]
tls_ignore_name_mismatch = true
```

`conf.d/20-google.toml`：

```toml
# 转写自原版 appsettings.google.json

[domains."ajax.googleapis.com"]
tls_sni = true
destination = "https://gapis.geekzu.org/ajax/"

[domains."fonts.googleapis.com"]
tls_sni = true
destination = "https://fonts.geekzu.org/"

[domains."themes.googleusercontent.com"]
tls_sni = true
destination = "https://gapis.geekzu.org/g-themes/"

[domains."fonts.gstatic.com"]
tls_sni = true
destination = "https://gapis.geekzu.org/g-fonts/"

[domains."*.gravatar.com"]
tls_ignore_name_mismatch = true
```

- [ ] **Step 4: 运行测试确认通过**

```
go test ./internal/config -run TestShippedConfig -v
```

Expected: `PASS`。

- [ ] **Step 5: 提交**

```
git add fastgithub.toml conf.d internal/config/shipped_test.go
git commit -m "feat(config): 随包默认配置与 golden 测试"
```

---

### Task 10: CLI 收尾——`config validate` 端到端

**Files:**
- Modify: `cmd/fastgithub/main.go`
- Test: `cmd/fastgithub/main_test.go`

**Interfaces:**
- Consumes: `config.Discover`、`config.Load`、`config.NewStore`
- Produces: `fastgithub config validate [--path 文件]`，成功输出 `配置有效: N 条域名规则，代理端口 X，UI 端口 Y`；失败输出 `错误: ...` 并以 1 退出

- [ ] **Step 1: 写失败测试**

追加到 `cmd/fastgithub/main_test.go`：

```go
func TestConfigValidateValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fastgithub.toml")
	if err := os.WriteFile(path, []byte("[server]\nproxy_port = 38457\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"config", "validate", "--path", path}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d，stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "配置有效") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestConfigValidateInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fastgithub.toml")
	if err := os.WriteFile(path, []byte("[server]\nproxy_port = 0\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"config", "validate", "--path", path}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit = %d，期望 1", code)
	}
	if !strings.Contains(stderr.String(), "proxy_port") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}
```

并在文件头补 `import`：`"os"`、`"path/filepath"`。

- [ ] **Step 2: 运行测试确认失败**

```
go test ./cmd/fastgithub -run TestConfigValidate -v
```

Expected: `FAIL`，`config` 子命令输出用法或 `undefined`。

- [ ] **Step 3: 写实现**

`cmd/fastgithub/main.go` 完整替换为：

```go
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"fastgithub-go/internal/app"
	"fastgithub-go/internal/config"
)

const usage = `用法: fastgithub <命令>

命令:
  config validate [--path 文件]        校验配置
  version                              打印版本信息
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 1
	}
	switch args[0] {
	case "version":
		if len(args) != 1 {
			fmt.Fprint(stderr, usage)
			return 1
		}
		fmt.Fprintln(stdout, app.BuildInfo())
		return 0
	case "config":
		if err := runConfig(args[1:], stdout); err != nil {
			fmt.Fprintln(stderr, "错误: "+err.Error())
			return 1
		}
		return 0
	default:
		fmt.Fprint(stderr, usage)
		return 1
	}
}

func runConfig(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "validate" {
		return fmt.Errorf("用法: fastgithub config validate [--path 文件]")
	}
	fs := flag.NewFlagSet("config validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	path := fs.String("path", "", "配置文件路径")
	if err := fs.Parse(args[1:]); err != nil {
		return fmt.Errorf("参数错误: %w", err)
	}

	mainPath, confDir, err := config.Discover(*path)
	if err != nil {
		return err
	}
	cfg, err := config.Load(mainPath, confDir)
	if err != nil {
		return err
	}
	store, err := config.NewStore(cfg)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "配置有效: %d 条域名规则，代理端口 %d，UI 端口 %d\n",
		len(store.Patterns()), cfg.Server.ProxyPort, cfg.Server.UIPort)
	return nil
}
```

- [ ] **Step 4: 运行全部测试**

```
go test ./... -v
```

Expected: 全绿。

- [ ] **Step 5: 端到端验证**

```
go build -o fastgithub.exe ./cmd/fastgithub
.\fastgithub.exe config validate
.\fastgithub.exe config validate --path .\fastgithub.toml
```

Expected: 输出 `配置有效: 15 条域名规则，代理端口 38457，UI 端口 38470`（数量以实际配置为准），退出码 0。

- [ ] **Step 6: 格式与静态检查**

```
gofmt -l .
go vet ./...
```

Expected: `gofmt` 无输出；`go vet` 无输出。

- [ ] **Step 7: 提交**

```
git add cmd/fastgithub internal/config
git commit -m "feat(config): config validate 端到端可用"
```

---

## 计划完成后的验证清单

- [ ] `go test ./...` 全绿
- [ ] `go vet ./...`、`gofmt -l .` 无输出
- [ ] `fastgithub config validate` 对随包配置返回 0
- [ ] 把 `proxy_port = 0` 写进临时配置，`config validate` 返回 1 且信息含 `proxy_port`
- [ ] `GOOS=linux go build ./cmd/fastgithub` 与 `GOOS=darwin go build ./cmd/fastgithub` 成功
- [ ] `git log --oneline` 可见 10 个任务提交