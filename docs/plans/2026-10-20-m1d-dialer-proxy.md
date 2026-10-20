# M1-D 拨号器与代理实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 `internal/dialer`（IP 选择、SNI、自定义证书校验、连接复用）与 `internal/mitm`（CONNECT/MITM/隧道/反向代理/PAC），并让 `fastgithub run` 在 38457 提供显式代理。

**Architecture:** `dialer` 从 `config.Store` 取域名规则、从 `resolver.Resolver` 取竞速后的 IP，`DialTLS` 用自定义 `VerifyConnection` 复刻原版 SAN 判定；`mitm.Server` 在 38457 上按首字节分派 CONNECT / 绝对 URI / PAC，CONNECT 命中规则时用本地 CA 做 MITM，否则盲隧道；反向代理用 `http.Transport`（`DialContext`/`DialTLSContext` 都走 dialer）。

**Tech Stack:** Go 1.22，仅标准库（`crypto/tls`/`crypto/x509`/`net`/`net/http`/`net/http/httputil` 语义、`bufio`/`context`/`errors`/`io`/`sync`），无第三方模块；`CGO_ENABLED=0`。

**Spec:** `docs/specs/2026-10-20-fastgithub-go-design.md` §7.2（dialer）、§7.4（mitm）、§5.3/§5.4（入口与数据流）、§9.2（回环集成测试）、§13（`@ipaddress` 语义）。

## Global Constraints

- 查询/拨号顺序：`domain.ip` 优先 → resolver 结果；同一 IP 去重。
- 连接超时 10s、TLS 握手 10s；隧道空闲超时 5min；缓冲区 32KB 池化。
- 证书校验：`InsecureSkipVerify=true` + `VerifyConnection`；链用系统根（测试可注入）；名称用设计文档的手写规则 `dnsName == host || (dnsName 以 * 开头 && host 以 dnsName[1:] 结尾)`；`tls_ignore_name_mismatch=true` 只跳过名称检查。
- SNI：`conf.SNIPattern()` 展开后作为 `tls.Config.ServerName`；值为 IP 字面量时 Go 不发送 SNI（RFC 6066），`@ipaddress` 等价于不发 SNI，必须用假 TLS 服务器断言。
- 反向代理保留原始 `Host`；`Destination` 用 `config.JoinDestination` 后拼接请求路径。
- 每个任务结束时 `go test ./...` 全绿、`go vet`/`gofmt` 无输出。

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `internal/dialer/dialer.go` | `Target`、`DialTCP`、`DialTLS`、IP 顺序 |
| `internal/dialer/tlsverify.go` | 链验证与手写名称匹配 |
| `internal/dialer/pool.go` | 空闲 TCP 连接复用 |
| `internal/dialer/transport.go` | `http.RoundTripper`（DialContext/DialTLSContext 走 dialer） |
| `internal/mitm/connect.go` | CONNECT 解析与响应 |
| `internal/mitm/sniff.go` | 前两字节 TLS 判定 |
| `internal/mitm/tunnel.go` | 双向复制与空闲超时 |
| `internal/mitm/reverse.go` | 反向代理、短路响应、Destination |
| `internal/mitm/pac.go` | PAC 文本生成 |
| `internal/mitm/proxy.go` | 38457 连接分派、MITM、单连接 HTTP |
| `internal/app/run.go` | 装配与监听 |
| `cmd/fastgithub/main.go` | `run` 子命令 |

---

### Task 1: dialer

**Files:** `internal/dialer/dialer.go`、`tlsverify.go`、`pool.go`、`transport.go`、`dialer_test.go`、`tlsverify_test.go`、`pool_test.go`

**Interfaces:**
```go
type Resolver interface { Lookup(ctx context.Context, domain string) ([]net.IP, error) }
type Target struct { Host string; Port int; Domain *config.Domain }
type Dialer struct { /* store, resolver, roots, timeouts */ }
func New(store *config.Store, resolver Resolver, roots *x509.CertPool) *Dialer
func (d *Dialer) DialTCP(ctx context.Context, t Target) (net.Conn, error)
func (d *Dialer) DialTLS(ctx context.Context, t Target) (*tls.Conn, error)
func NewTransport(store *config.Store, d *Dialer) *http.Transport
func NewPool(maxIdle int, idleTimeout time.Duration) *Pool
```

要点：
- `ips(ctx, t)`：`t.Domain.IP` 解析后置首；host 是 IP 字面量时直接用；否则 `resolver.Lookup`；去重；resolver 失败但有 `conf.IP` 时仍可用。
- `DialTCP`：按 IP 顺序逐个 `DialContext`，每个 10s，聚合错误。
- `DialTLS`：`dialFirst` 返回实际 IP；`ServerName = config.ExpandSNI(conf.SNIPattern(), host, ip)`；`VerifyConnection` 调 `verifyParsed(certs, host, ignore, roots)`；`HandshakeContext` 10s。
- `tlsverify.go`：解析链、`Intermediates`、`Verify`（ServerAuth）；名称匹配按设计规则，IP 目标比较 IP SAN。
- `transport.go`：`http.Transport{DialContext, DialTLSContext, ForceAttemptHTTP2:false, TLSNextProto: 空 map}`；两者都从 `addr` 解析 host/port 并查 Store。
- `pool.go`：`map[string][]*idleConn` + 过期清理；测试 Get/Put/过期/关闭。

**测试：** 假 TLS 服务器记录 `ConnectionState().ServerName`，断言 `@domain`/`@ipaddress`/`tls_sni=false` 三种；名称不匹配两路径；`ips` 顺序；pool。

---

### Task 2: mitm 基础件（CONNECT、sniff、tunnel、PAC、reverse）

**Files:** `internal/mitm/connect.go`、`sniff.go`、`tunnel.go`、`pac.go`、`reverse.go` + 对应 `_test.go`

**Interfaces:**
```go
func ReadConnect(br *bufio.Reader) (host string, port int, err error)
func WriteConnectEstablished(w io.Writer) error
func WriteConnectError(w io.Writer, status int) error
func IsTLS(b []byte) bool
func Tunnel(ctx context.Context, client, upstream net.Conn, idle time.Duration) error
func BuildPAC(patterns []string, proxyHost string) []byte
func NewReverseProxy(store *config.Store, transport http.RoundTripper) *ReverseProxy
```

要点：
- CONNECT 读到空行，累计上限 8KB；只接受 `CONNECT host:port HTTP/1.x`。
- 隧道：两个 goroutine + 32KB 池化缓冲；每次 Read 前 `SetReadDeadline(now+idle)`；任一方向结束关闭两端。
- PAC：与设计逐字同构，pattern 用 `store.Patterns()`，代理地址用请求 Host，缺省 `127.0.0.1:38457`。
- 反向代理：查 Store；`Response` 命中直接写短路响应；否则计算 `Destination`（`JoinDestination` + 路径拼接）、保留 `Host`、去掉 hop-by-hop 头、`RoundTrip`、复制响应；错误回 502。

**测试：** CONNECT 解析与超限；PAC 逐字 golden；短路响应；Destination 路径拼接；隧道 echo。

---

### Task 3: mitm 服务器与 MITM

**Files:** `internal/mitm/proxy.go`、`singleconn.go`、`proxy_test.go`

**Interfaces:**
```go
type Server struct { /* store, dialer, ca, reverse, idle */ }
func NewServer(store *config.Store, d *dialer.Dialer, ca *certs.Authority, reverse http.Handler, idle time.Duration) *Server
func (s *Server) ServeConn(ctx context.Context, conn net.Conn)
```

要点：
- `ServeConn`：`Peek(2)` 匹配 `0x16 0x03` → 透明 TLS（无 SNI 时拒绝）；`Peek(7) == "CONNECT"` → CONNECT；否则单连接 `http.Server` 处理 PAC/绝对 URI。
- CONNECT 命中 Store 规则 → 回 200 后 `tls.Server`，`GetCertificate` 按 SNI（空则用 CONNECT host）取叶子；否则先拨上游再回 200 并隧道。
- MITM 握手后把 `*tls.Conn` 交给单连接 `http.Server`，handler 为 reverse。
- `bufferedConn` 保留 `bufio.Reader` 中已 peek 的字节。

**测试：** CONNECT 命中 → MITM → 假上游端到端；未命中 → 隧道；PAC 经 38457 GET。

---

### Task 4: app 装配与 CLI

**Files:** `internal/app/run.go`、`run_test.go`、`cmd/fastgithub/main.go`、`main_test.go`

**Interfaces:**
```go
func Run(ctx context.Context, cfg *config.Config, store *config.Store) error
```
- `Run`：`certs.LoadOrCreate(certs.DefaultDir)` → `resolver.New(cfg.DNS)` → `dialer.New` → `dialer.NewTransport` → `mitm.NewReverseProxy` → `mitm.NewServer` → 监听 `127.0.0.1:<proxy_port>` → accept 循环，ctx 取消时关闭监听并返回。
- CLI：`fastgithub run [--path 文件]`，启动后打印 `代理已启动: 127.0.0.1:<port>`；SIGINT/SIGTERM 优雅退出。

**测试：** 回环集成——测试 CA、假 HTTPS 上游、配置 `ip=127.0.0.1`，HTTP 客户端走代理 CONNECT，断言正文与证书；`run` 的用法/错误路径。

---

## 验证清单

- [ ] `go test ./...` 全绿，`go test -race ./internal/dialer ./internal/mitm ./internal/app` 通过
- [ ] `go vet`、`gofmt` 无输出
- [ ] 六目标交叉编译
- [ ] 手工：`fastgithub run` 后用 `curl -x http://127.0.0.1:38457 https://github.com`（需网络与 CA 信任）
- [ ] `@ipaddress` 假 TLS 服务器断言 SNI 为空
