# M1-C DNS 解析器实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 `internal/resolver`：DoH 优先、明文 DNS（TCP）回退，A/AAAA 合并，TTL 夹逼与负缓存，并发去重，候选 IP 按 TCP 时延竞速排序。

**Architecture:** 自实现 DNS 报文编解码（只读压缩指针、CNAME 链）→ `dohClient` 走 `application/dns-message`，`queryTCP` 走 2 字节长度前缀 → `Resolver.Lookup` 并发查询 A/AAAA，经 `flightGroup` 去重后写入正/负缓存 → `racer` 对候选 IP 并发拨号 443 并按时延排序，结果写入 IP→时延缓存。

**Tech Stack:** Go 1.22（工具链 1.27.1），仅标准库（`crypto/rand`/`encoding/binary`/`net`/`net/http`/`context`/`errors`/`sort`/`sync`/`time`），无第三方模块；`CGO_ENABLED=0`。

**Spec:** `docs/specs/2026-10-20-fastgithub-go-design.md` §7.1（resolver）、§6.2/§6.3（`dns.*` 配置）、§9.1（单元测试）、§9.3（构建验证）。

## Global Constraints

- 模块路径 `fastgithub-go`，`go.mod` 声明 `go 1.22`，**不出现任何 `require`**。
- 查询链顺序：`dns.doh` 配置顺序 → `dns.fallback` 配置顺序（只用 TCP）；DoH 传输/响应失败后冷却 30s。
- 超时：`dns.timeout` 是整次解析的总预算（A/AAAA 并发共享）；调用方取消与共享查询解耦，且不写负缓存。
- TTL：正缓存夹逼 `30s ~ 10min`，CNAME 链取整链最小 TTL；负缓存非 socket 错误 30s、`*net.OpError` 10min。
- 竞速：每个 IP 并发 TCP 拨号 `443`，单次上限 2s；全部失败时保持原序。
- 面向用户的错误信息用中文；错误需可 `errors.Is/As` 判断（`net.Error` 用于 socket 分类）。
- 每个任务结束时 `go test ./...` 全绿、`go vet ./...` 无输出、`gofmt -l .` 无输出。

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `internal/resolver/dnsmsg.go` | 报文结构、查询构造、响应解析、名字压缩、CNAME 链 |
| `internal/resolver/doh.go` | DoH POST 客户端 |
| `internal/resolver/resolve.go` | `Resolver`、`Lookup`、A/AAAA 合并、TCP 回退、错误分类 |
| `internal/resolver/cache.go` | 正/负缓存、IP→时延缓存 |
| `internal/resolver/singleflight.go` | 同域名并发查询去重 |
| `internal/resolver/race.go` | 候选 IP 并发拨号与时延排序 |
| `internal/resolver/*_test.go` | 报文 golden、DoH/回退、TTL、负缓存、去重、竞速 |

---

### Task 1: DNS 报文编解码

**Files:**
- Create: `internal/resolver/dnsmsg.go`
- Test: `internal/resolver/dnsmsg_test.go`

**Interfaces:**
- Produces:
  - `func buildQuery(domain string, qtype uint16) ([]byte, uint16, error)`
  - `func parseMessage(data []byte) (*dnsMessage, error)`
  - `func (m *dnsMessage) addresses(qname string, qtype uint16) ([]net.IP, uint32)`
  - 常量 `typeA=1`、`typeCNAME=5`、`typeAAAA=28`、`classIN=1`
  - `var errNXDOMAIN = errors.New("域名不存在 (NXDOMAIN)")`

- [ ] **Step 1: 写失败测试**

```go
func TestBuildQueryGolden(t *testing.T) {
	msg, id, err := buildQueryWithID("example.com", typeA, 0x1234)
	if err != nil {
		t.Fatal(err)
	}
	if id != 0x1234 {
		t.Fatalf("id = %#x", id)
	}
	want := []byte{
		0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0,
		0x00, 0x01, 0x00, 0x01,
	}
	if !bytes.Equal(msg, want) {
		t.Fatalf("query = % x\nwant  = % x", msg, want)
	}
}

func TestParseCompressedCNAME(t *testing.T) {
	resp := buildTestResponse(t, "example.com", typeA, []testRecord{
		{name: "example.com", typ: typeCNAME, ttl: 60, target: "cdn.example.net"},
		{name: "cdn.example.net", typ: typeA, ttl: 30, ip: net.ParseIP("1.2.3.4")},
	}, 0)
	m, err := parseMessage(resp)
	if err != nil {
		t.Fatal(err)
	}
	ips, ttl := m.addresses("example.com", typeA)
	if len(ips) != 1 || !ips[0].Equal(net.ParseIP("1.2.3.4")) || ttl != 30 {
		t.Fatalf("ips=%v ttl=%d", ips, ttl)
	}
}

func TestParseRejectsPointerLoop(t *testing.T) {
	// 问题名指向自身：12 字节头 + 0xC0 0x0C
	msg := make([]byte, 14)
	msg[4], msg[5] = 0, 1
	msg[12], msg[13] = 0xC0, 0x0C
	if _, err := parseMessage(msg); err == nil {
		t.Fatal("压缩指针循环应报错")
	}
}

func TestSplitNameErrors(t *testing.T) {
	for _, name := range []string{"", "a..b", strings.Repeat("a", 64) + ".com"} {
		if _, err := splitName(name); err == nil {
			t.Fatalf("splitName(%q) 应报错", name)
		}
	}
}
```

`buildTestResponse` 放在 `dnsmsg_test.go`，按问题名/QTYPE 构造响应，第二个答案用压缩指针指向第一个答案的 rdata 名字（验证只读压缩）。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/resolver -run 'TestBuildQueryGolden|TestParseCompressedCNAME|TestParseRejectsPointerLoop|TestSplitNameErrors' -count=1`
Expected: 编译失败（未定义）。

- [ ] **Step 3: 实现 `dnsmsg.go`**

要点（完整实现）：

```go
const (
	typeA     = 1
	typeCNAME = 5
	typeAAAA  = 28
	classIN   = 1
)

var errNXDOMAIN = errors.New("域名不存在 (NXDOMAIN)")

type dnsQuestion struct {
	Name string
	Type uint16
}

type dnsRecord struct {
	Name   string
	Type   uint16
	Class  uint16
	TTL    uint32
	Target string
	IP     net.IP
}

type dnsMessage struct {
	ID        uint16
	Flags     uint16
	Questions []dnsQuestion
	Answers   []dnsRecord
}

func (m *dnsMessage) rcode() int { return int(m.Flags & 0xF) }

func buildQuery(domain string, qtype uint16) ([]byte, uint16, error) {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, 0, fmt.Errorf("生成 DNS 请求 ID 失败: %w", err)
	}
	return buildQueryWithID(domain, qtype, binary.BigEndian.Uint16(b[:]))
}

func buildQueryWithID(domain string, qtype uint16, id uint16) ([]byte, uint16, error) {
	labels, err := splitName(domain)
	if err != nil {
		return nil, 0, err
	}
	msg := make([]byte, 12, 12+len(domain)+6)
	binary.BigEndian.PutUint16(msg[0:2], id)
	binary.BigEndian.PutUint16(msg[2:4], 0x0100) // RD
	binary.BigEndian.PutUint16(msg[4:6], 1)
	for _, label := range labels {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0)
	msg = binary.BigEndian.AppendUint16(msg, qtype)
	msg = binary.BigEndian.AppendUint16(msg, classIN)
	return msg, id, nil
}
```

`splitName`：去尾点、拒绝空标签/超 63 字节标签/总长超 255。

`parseMessage`：校验 12 字节头，读 QD/AN，解析问题与答案；A 要求 rdlen=4，AAAA 要求 16，CNAME 用 `decodeName` 解出 `Target`；跳过 authority/additional。

`decodeName`：逐标签读取；`0xC0` 指针跳转，记录首次跳转后的返回位置；指针跳转上限 128，防循环；返回小写化、无尾点的名字。

`addresses`：按小写名字建索引，从 `qname` 起沿 CNAME 链（最多 8 跳）收集匹配 `qtype` 的 A/AAAA，返回最小 TTL。

- [ ] **Step 4: 运行测试并提交**

Run: `go test ./internal/resolver -count=1 && go vet ./... && gofmt -l .`

```bash
git add internal/resolver/dnsmsg.go internal/resolver/dnsmsg_test.go
git commit -m "feat(resolver): DNS 报文编解码"
```

---

### Task 2: DoH 与 TCP 回退

**Files:**
- Create: `internal/resolver/doh.go`
- Create: `internal/resolver/resolve.go`（先实现 `queryTCP` 与 `lookupType`）
- Test: `internal/resolver/doh_test.go`

**Interfaces:**
- Consumes: `buildQuery`、`parseAnswer`（Task 1 的 `parseMessage`/`addresses`）。
- Produces:
  - `type dohClient struct { endpoints []string; client *http.Client }`
  - `func (c *dohClient) query(ctx context.Context, endpoint string, msg []byte) ([]byte, error)`
  - `func queryTCP(ctx context.Context, server string, msg []byte) ([]byte, error)`
  - `func parseAnswer(data []byte, id uint16, domain string, qtype uint16) ([]net.IP, uint32, error)`

- [ ] **Step 1: 写失败测试**

用 `httptest.NewServer` 断言请求方法、`Content-Type: application/dns-message`、请求体是合法查询，并返回构造好的响应；再加一个 TCP 假 DNS 服务器验证长度前缀：

```go
func TestDoHQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/dns-message" {
			t.Errorf("method=%s content-type=%s", r.Method, r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(makeResponseForQuery(t, body, []string{"1.2.3.4"}, 60, 0))
	}))
	defer srv.Close()
	c := &dohClient{endpoints: []string{srv.URL}, client: srv.Client()}
	msg, id, err := buildQueryWithID("example.com", typeA, 0x1234)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := c.query(context.Background(), srv.URL, msg)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	ips, ttl, err := parseAnswer(resp, id, "example.com", typeA)
	if err != nil || len(ips) != 1 || ttl != 60 {
		t.Fatalf("ips=%v ttl=%d err=%v", ips, ttl, err)
	}
}

func TestQueryTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go serveOneTCP(t, ln)
	msg, id, _ := buildQueryWithID("example.com", typeA, 0x2222)
	resp, err := queryTCP(context.Background(), ln.Addr().String(), msg)
	if err != nil {
		t.Fatalf("queryTCP: %v", err)
	}
	if _, _, err := parseAnswer(resp, id, "example.com", typeA); err != nil {
		t.Fatal(err)
	}
}
```

`parseAnswer` 还应覆盖：ID 不匹配、QR 缺失、NXDOMAIN 返回 `errNXDOMAIN`、SERVFAIL 报错、无记录报错。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/resolver -run 'TestDoHQuery|TestQueryTCP|TestParseAnswer' -count=1`
Expected: 编译失败。

- [ ] **Step 3: 实现**

`doh.go`：

```go
func (c *dohClient) query(ctx context.Context, endpoint string, msg []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(msg))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 65535))
	if err != nil {
		return nil, err
	}
	if len(body) < 12 {
		return nil, fmt.Errorf("DNS 响应过短")
	}
	return body, nil
}
```

`resolve.go` 中的 `queryTCP` 与 `parseAnswer`：

```go
func queryTCP(ctx context.Context, server string, msg []byte) ([]byte, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", server)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}
	var length [2]byte
	binary.BigEndian.PutUint16(length[:], uint16(len(msg)))
	if _, err := conn.Write(append(length[:], msg...)); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(conn, length[:]); err != nil {
		return nil, err
	}
	n := int(binary.BigEndian.Uint16(length[:]))
	if n == 0 {
		return nil, fmt.Errorf("DNS 响应为空")
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(conn, body); err != nil {
		return nil, err
	}
	return body, nil
}

func parseAnswer(data []byte, id uint16, domain string, qtype uint16) ([]net.IP, uint32, error) {
	m, err := parseMessage(data)
	if err != nil {
		return nil, 0, err
	}
	if m.ID != id {
		return nil, 0, fmt.Errorf("DNS 响应 ID 不匹配")
	}
	if m.Flags&0x8000 == 0 {
		return nil, 0, fmt.Errorf("DNS 响应缺少 QR 位")
	}
	if len(m.Questions) > 0 && !strings.EqualFold(strings.TrimSuffix(m.Questions[0].Name, "."), domain) {
		return nil, 0, fmt.Errorf("DNS 响应问题名不匹配")
	}
	switch m.rcode() {
	case 0:
	case 3:
		return nil, 0, errNXDOMAIN
	default:
		return nil, 0, fmt.Errorf("DNS 返回错误码 %d", m.rcode())
	}
	ips, ttl := m.addresses(domain, qtype)
	if len(ips) == 0 {
		return nil, 0, fmt.Errorf("无 A/AAAA 记录")
	}
	return ips, ttl, nil
}
```

- [ ] **Step 4: 运行测试并提交**

Run: `go test ./internal/resolver -count=1 && go vet ./... && gofmt -l .`

```bash
git add internal/resolver/doh.go internal/resolver/resolve.go internal/resolver/doh_test.go
git commit -m "feat(resolver): DoH 与 TCP 回退"
```

---

### Task 3: 缓存、去重与竞速

**Files:**
- Create: `internal/resolver/cache.go`
- Create: `internal/resolver/singleflight.go`
- Create: `internal/resolver/race.go`
- Test: `internal/resolver/cache_test.go`, `internal/resolver/singleflight_test.go`, `internal/resolver/race_test.go`

**Interfaces:**
- Produces:
  - `func newIPCache(max int) *ipCache`、`get/putPositive/putNegative`
  - `func newLatencyCache(max int) *latencyCache`、`get/put`
  - `func newFlightGroup() *flightGroup`、`func (g *flightGroup) Do(key string, fn func() ([]net.IP, error)) ([]net.IP, error)`
  - `type racer struct { timeout time.Duration; port string; latency *latencyCache; dial func(context.Context, string, string) (net.Conn, error); now func() time.Time }`
  - `func (r *racer) Sort(ctx context.Context, ips []net.IP) []net.IP`

- [ ] **Step 1: 写失败测试**

- 缓存：正缓存过期、负缓存保留错误、达到上限重置、返回副本。
- 去重：8 个并发 `Do("k", fn)`，`fn` 只执行一次。
- 竞速：注入 `dial`，`1.1.1.1` 慢 50ms、`2.2.2.2` 快、`3.3.3.3` 失败 → 顺序 `2.2.2.2,1.1.1.1,3.3.3.3`；全部失败 → 原序；成功时延写入缓存，第二次不拨号。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/resolver -run 'TestIPCache|TestFlight|TestRacer' -count=1`
Expected: 编译失败。

- [ ] **Step 3: 实现**

`cache.go` 的语义：

```go
type ipCacheEntry struct {
	ips     []net.IP
	err     error
	expires time.Time
}

func (c *ipCache) get(domain string, now time.Time) ([]net.IP, error, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[domain]
	if !ok {
		return nil, nil, false
	}
	if !now.Before(e.expires) {
		delete(c.entries, domain)
		return nil, nil, false
	}
	return copyIPs(e.ips), e.err, true
}

func (c *ipCache) put(domain string, e ipCacheEntry, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max {
		c.entries = map[string]ipCacheEntry{}
	}
	e.expires = now.Add(e.ttl)
	c.entries[domain] = e
}
```

（`putPositive`/`putNegative` 是 `put` 的薄封装，分别保存 `ips` 与 `err`。）

`latencyCache`：`map[string]latencyEntry`，`get(ip, now)` 返回 `(latency, ok, found)`；`put` 成功保存 5min、失败保存 1min；上限 4096，满时重置。

`singleflight.go` 按计划开头的 `flightCall` 实现：`Do` 在 map 中登记 `done` channel，首个调用执行 `fn`，其余等待同一 channel；完成后删除 key。

`race.go` 按以下逻辑实现：

```go
func (r *racer) Sort(ctx context.Context, ips []net.IP) []net.IP {
	now := r.now()
	type probe struct {
		idx     int
		ip      net.IP
		latency time.Duration
		ok      bool
	}
	probes := make([]probe, len(ips))
	var wg sync.WaitGroup
	for i, ip := range ips {
		probes[i] = probe{idx: i, ip: ip}
		if lat, ok, found := r.latency.get(ip.String(), now); found {
			probes[i].latency, probes[i].ok = lat, ok
			continue
		}
		wg.Add(1)
		go func(p *probe) {
			defer wg.Done()
			start := r.now()
			attemptCtx, cancel := context.WithTimeout(ctx, r.timeout)
			conn, err := r.dial(attemptCtx, "tcp", net.JoinHostPort(p.ip.String(), r.port))
			cancel()
			if err != nil {
				r.latency.put(p.ip.String(), 0, false, now)
				return
			}
			conn.Close()
			p.ok = true
			p.latency = r.now().Sub(start)
			r.latency.put(p.ip.String(), p.latency, true, now)
		}(&probes[i])
	}
	wg.Wait()
	anyOK := false
	for _, p := range probes {
		if p.ok {
			anyOK = true
		}
	}
	if !anyOK {
		return ips
	}
	sort.SliceStable(probes, func(i, j int) bool {
		if probes[i].ok != probes[j].ok {
			return probes[i].ok
		}
		if !probes[i].ok {
			return probes[i].idx < probes[j].idx
		}
		if probes[i].latency != probes[j].latency {
			return probes[i].latency < probes[j].latency
		}
		return probes[i].idx < probes[j].idx
	})
	out := make([]net.IP, len(ips))
	for i, p := range probes {
		out[i] = p.ip
	}
	return out
}
```

- [ ] **Step 4: 运行测试并提交**

Run: `go test ./internal/resolver -count=1 -race && go vet ./... && gofmt -l .`

```bash
git add internal/resolver/cache.go internal/resolver/singleflight.go internal/resolver/race.go internal/resolver/cache_test.go internal/resolver/singleflight_test.go internal/resolver/race_test.go
git commit -m "feat(resolver): 缓存、并发去重与 IP 竞速"
```

---

### Task 4: Resolver 组装与查询链

**Files:**
- Modify: `internal/resolver/resolve.go`
- Test: `internal/resolver/resolve_test.go`

**Interfaces:**
- Consumes: 前面全部组件、`config.DNS`。
- Produces:
  - `func New(cfg config.DNS) *Resolver`
  - `func (r *Resolver) Lookup(ctx context.Context, domain string) ([]net.IP, error)`

- [ ] **Step 1: 写失败测试**

覆盖：

1. **DoH 成功**：假 DoH 同时响应 A/AAAA，`Lookup` 返回两类地址并去重。
2. **DoH 失败回退 TCP**：第一个 DoH 返回 500，TCP 假服务器返回 A；断言使用回退且结果正确。
3. **TTL 夹逼**：响应 TTL=1，固定时钟下 29s 内第二次 `Lookup` 不再请求，31s 后重新请求（请求计数）。
4. **负缓存**：NXDOMAIN 后 30s 内不再请求；`negativeTTL(net.OpError)` 返回 10min。
5. **并发去重**：8 个并发 `Lookup` 只产生 A+AAAA 两次上游请求。
6. **域名规范化**：`GitHub.COM.` 与 `github.com` 共用缓存。

- [ ] **Step 2: 运行确认失败**

Run: `go test ./internal/resolver -run 'TestResolver' -count=1`
Expected: 编译失败（未定义 `New`）。

- [ ] **Step 3: 实现**

```go
const (
	minCacheTTL          = 30 * time.Second
	maxCacheTTL          = 10 * time.Minute
	nonSocketNegativeTTL = 30 * time.Second
	socketNegativeTTL    = 10 * time.Minute
	racePort             = "443"
	raceTimeout          = 2 * time.Second
)

type Resolver struct {
	doh      *dohClient
	fallback []string
	timeout  time.Duration
	cache    *ipCache
	latency  *latencyCache
	group    *flightGroup
	racer    *racer
	now      func() time.Time
}

func New(cfg config.DNS) *Resolver {
	timeout := cfg.Timeout.Std()
	if timeout <= 0 {
		timeout = 4 * time.Second
	}
	r := &Resolver{
		doh:      &dohClient{endpoints: append([]string(nil), cfg.DoH...), client: &http.Client{Timeout: timeout}},
		fallback: append([]string(nil), cfg.Fallback...),
		timeout:  timeout,
		cache:    newIPCache(4096),
		latency:  newLatencyCache(4096),
		group:    newFlightGroup(),
		now:      time.Now,
	}
	r.racer = &racer{
		timeout: raceTimeout,
		port:    racePort,
		latency: r.latency,
		dial:    (&net.Dialer{}).DialContext,
		now:     func() time.Time { return r.now() },
	}
	return r
}
```

`Lookup`：规范化域名（小写、去尾点）→ 查缓存 → `group.Do`（内部二次查缓存）→ `resolve`。

`resolve`：并发查询 A 与 AAAA；任一成功即合并去重，TTL 取成功结果的最小值（用 `minSet` 处理 TTL=0）；全部失败时写入负缓存（`negativeTTL` 按 `errors.As(&net.Error)` 分类）；成功时 `racer.Sort` 后写正缓存（`clampTTL`）。

`lookupType`：按 `doh.endpoints` 顺序尝试（每次 `context.WithTimeout(r.timeout)`）；DoH 全部失败后按 `fallback` 顺序 `queryTCP`；`errNXDOMAIN` 立即返回，其他错误继续下一个源；全部失败用 `errors.Join` 聚合。

`negativeTTL`：

```go
func negativeTTL(err error) time.Duration {
	var ne net.Error
	if errors.As(err, &ne) {
		return socketNegativeTTL
	}
	return nonSocketNegativeTTL
}
```

- [ ] **Step 4: 运行测试并提交**

Run: `go test ./... -count=1 && go test -race ./internal/resolver -count=1 && go vet ./... && gofmt -l .`

```bash
git add internal/resolver/resolve.go internal/resolver/resolve_test.go
git commit -m "feat(resolver): 查询链、TTL 夹逼与负缓存"
```

---

### Task 5: 文档与收尾

**Files:**
- Modify: `README.md`
- Modify: `docs/handoff-2026-10-20.md`

- [ ] **Step 1: 更新 README 状态**

把「配置内核（M1-A）与证书模块（M1-B）已完成」改为「M1-A 配置内核、M1-B 证书、M1-C DNS 解析器已完成」，里程碑表状态同步。

- [ ] **Step 2: 更新交接文档**

追加「M1-C 完成状态」：文件、查询链语义、缓存参数、验证命令、下一里程碑 M1-D 及其待确认项（`@ipaddress` 作 SNI）。

- [ ] **Step 3: 全量验证并提交**

Run: `go test ./... -count=1 && go test -race ./internal/resolver ./internal/config ./internal/certs -count=1 && go vet ./... && gofmt -l .`
Run: `for t in windows/amd64 windows/arm64 linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -o /tmp/m1c-${t%/*}-${t#*/} ./cmd/fastgithub || exit 1; done`

```bash
git add README.md docs/handoff-2026-10-20.md
git commit -m "docs: 记录 M1-C 完成状态"
```

---

## 计划完成后的验证清单

- [ ] `go test ./...` 全绿，`go test -race ./internal/resolver` 通过
- [ ] `go vet`、`gofmt` 无输出
- [ ] 六目标交叉编译成功
- [ ] 假 DoH/假 TCP 服务器测试覆盖：回退顺序、TTL 夹逼、两类负缓存、并发去重、竞速排序
- [ ] 不引入第三方依赖，`go.mod` 无 `require`
