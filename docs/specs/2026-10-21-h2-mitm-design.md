# MITM 双栈 HTTP/2 设计（v0.4）

> 状态：设计定稿，待实现
> 前置：v0.3.6（`5687dd3`）
> 相关：[2026-10-20-fastgithub-go-design.md](2026-10-20-fastgithub-go-design.md)

## 1. 背景

FastGithub 的价值是"让 GitHub 变快"。但当前实现对**被接管的所有域名**都把连接降到 HTTP/1.1，而没被接管的盲隧道反而保留原生 h2——恰好把最需要加速的流量放在了最慢的协议上。

三处证据（缺一不可）：

| 位置 | 现状 | 后果 |
|---|---|---|
| `internal/dialer/transport.go:43-44` | `ForceAttemptHTTP2: false`，且 `TLSNextProto` 被赋为空 map | 显式关闭 h2，回源永远 h1.1 |
| `internal/dialer/dialer.go` `DialTLS` | `tls.Config` 无 `NextProtos` | 到上游的 ALPN 不通告 |
| `internal/mitm/proxy.go:190,210` | 服务端 `tls.Config` 无 `NextProtos` | 浏览器看到的 MITM 站点无 ALPN，按 h1.1 处理 |

GitHub 页面元素多、并发请求密集，h1.1 的队头阻塞直接吃掉首屏时间。这是审查中发现的唯一一个"直接削弱产品卖点"的缺陷。

### 1.1 为什么不能走 SNI 直通

`conf.d/10-github.toml` 中 `github.com` 与 `api.github.com` 都配置了 `tls_sni = false`——回源时**故意不发 SNI**，靠 `tls_ignore_name_mismatch` 容忍证书名不匹配。这意味着必须终结 TLS 才能控制 SNI，纯 SNI 代理（透传 ClientHello）在这里不成立。所以 h2 只能在 MITM 之上实现。

### 1.2 为什么不做 DNS 多源并行合并

`resolver.resolve` 已经并行查询 A/AAAA；多个 DNS 源之间是"按优先级串行 + 失败切换"。并行合并会把可能被污染的源的结果混进候选池，与"净化 DNS"的产品前提冲突。若日后要降延迟，只能做 hedged 请求（第一源 200ms 未响应才启动下一源），优先级顺序不变。本设计不做。

## 2. 目标与非目标

**目标**

1. 被 MITM 的域名在浏览器 ↔ MITM ↔ 上游两段都使用 HTTP/2（客户端不支持时自动降级）。
2. HTTP/1.1 客户端、WebSocket、大文件流式下载行为不变。
3. 提供 `[server] http2` 开关，`false` 时行为与 v0.3.6 逐字节等价（同一代码路径，不走另一套实现）。

**非目标**

- HTTP/3 / QUIC
- MITM 数据面重构（共享 server、按域名连接池、请求优先级调度）
- DNS 源并行合并、连接预热

## 3. 设计

### 3.1 组件改动

| 包 | 改动 |
|---|---|
| `internal/dialer` | `DialTLS` 的 `tls.Config` 增加 `NextProtos: ["h2","http/1.1"]`；`NewTransport` 增加选项控制是否启用 h2；新增强制 h1 的 transport 构造函数 |
| `internal/mitm` | `serveMITM` / `serveTransparentTLS` 的服务端 `tls.Config` 增加 `NextProtos`；`serveOneHTTP` 改用 `srv.ServeTLS` 以注册 h2 处理器 |
| `internal/app` | 构造 h2 与 h1 两个 transport，注入 reverse proxy |
| `internal/config` | 新增 `[server] http2`（bool，默认 `true`） |

### 3.2 服务端如何拿到 h2

`http.Server.Serve` 不会注册 h2 处理器，只有 `ServeTLS` 内部会调用 `setupHTTP2_ServeTLS`。

**已用 spike 验证（Go 1.26.5）的正确做法**：不要自己先握手，把**原始 `net.Conn` 直接交给 `ServeTLS`**，由它完成握手（`GetCertificate` 按 SNI 选叶子证书）并注册 h2：

```go
srv := &http.Server{Handler: handler, TLSConfig: cfg, ReadHeaderTimeout: 10 * time.Second}
go srv.ServeTLS(&singleConnListener{conn: rawConn}, "", "")
```

**已验证的错误做法**：先 `tls.Server(...).Handshake()` 再把 `*tls.Conn` 交给 `ServeTLS`。Go 1.26 的 `tls.Server` 不再原样复用已包装连接，`ServeTLS` 会二次握手并报
`http: TLS handshake error ... first record does not look like a TLS handshake`。当前 `serveOneHTTP` 的"先握手再托管"结构必须一并改掉。

`TLSConfig` 已有 `GetCertificate` 时 `ServeTLS("", "")` 不会尝试加载证书文件。若该路径在目标 Go 版本上失效，备选是显式使用 `http2.Server`——但那需要 `golang.org/x/net/http2`，会破坏零依赖约束，因此优先标准库路径。

**实现状态（v0.4.0）**：已按上述方式落地。`serveMITM` / `serveTransparentTLS` 统一走 `serveOneTLS`，原始连接直接交给 `ServeTLS`；`serveConn` 用 `ConnState` 等待连接关闭，握手失败由客户端感知，`ErrorLog` 指向 `io.Discard` 避免刷屏。端到端测试 `TestMITMHTTP2EndToEnd` 断言浏览器侧和上游侧都是 h2。

**关闭 h2 的坑（v0.4.1 修复）**：`ServeTLS` 在 `TLSNextProto` 为 nil 时会自动把 `h2` 追加到 `NextProtos` 并注册处理器。因此 `http2=false` 时必须同时把 `Server.TLSNextProto` 设为非 nil 空 map，仅从 `NextProtos` 移除 h2 无效。

### 3.3 上游 transport

```go
// h2 开启
&http.Transport{
    DialTLSContext:     d.DialTLSH2, // 通告 h2,http/1.1
    ForceAttemptHTTP2:  true,
    // 不设置 TLSNextProto
}
// h2 关闭（等价 v0.3.6）
&http.Transport{
    DialTLSContext:     d.DialTLS,   // 不发送 ALPN
    ForceAttemptHTTP2:  false,
    TLSNextProto:       map[string]func(string, *tls.Conn) http.RoundTripper{},
}
```

自定义 `DialTLSContext` 默认会关闭 h2，必须显式 `ForceAttemptHTTP2=true`；同时**不能**再给 `TLSNextProto` 赋空 map，否则会覆盖已注册的 h2 处理器。这两行必须成对修改，是本设计最容易踩的坑。

### 3.4 WebSocket / Upgrade

Go 的 h2 服务器默认不宣告 `SETTINGS_ENABLE_CONNECT_PROTOCOL`（`net/http/h2_bundle.go` 中 `http2disableExtendedConnectProtocol = true`，注释说明正是为了避免 WebSocket-over-h2 的兼容问题）。浏览器的行为是：发现服务端不支持 RFC 8441，就为 WebSocket **另开一条 h1.1 连接**。

因此服务端侧不需要特殊处理；需要处理的是**回源侧**：`Upgrade` 请求不能被塞进 h2 transport（h2 无 Upgrade 语义）。reverse proxy 检测到 `r.Header.Get("Upgrade") != ""` 时改用 h1 专用 transport。

### 3.5 数据流

```
浏览器 ──TLS(ALPN h2)──▶ MITM 终结 ──h2──▶ transport ──TLS(ALPN h2)──▶ 真实 IP
        ├ ALPN 仅 h1.1 → 下游 h1.1；回源仍 h2（net/http 负责协议转换）
        └ Upgrade 请求 → 下游 h1.1 + 回源 h1.1
```

## 4. 错误处理与回退

| 场景 | 行为 |
|---|---|
| 上游不支持 h2 | ALPN 协商为 `http/1.1`，transport 走默认 h1 路径，无额外分支 |
| h2 流级错误 | `http.Server` 重置该 stream，同连接其他请求不受影响 |
| `http2 = false` | 使用 h1 专用 transport，路径与 v0.3.6 完全一致 |
| 客户端 ALPN 只有 h1.1 | 正常服务，无告警 |
| 流量统计 | 按请求计（`countingReader` / `countingWriter`），h2 多路复用下互不干扰 |

## 5. 测试计划

| # | 用例 | 断言 |
|---|---|---|
| 1 | ALPN 协商（h2-only / h1-only 客户端） | 选中协议符合预期 |
| 2 | 端到端 h2：`httptest` h2 上游经 MITM | 上游 handler 看到 `r.ProtoMajor == 2` |
| 3 | WebSocket 回归：hijack 端点 101 Upgrade | 握手成功、双向字节透传 |
| 4 | 大响应 1 MB | 内容完整、无截断 |
| 5 | `http2=false` | 上游看到 h1.1 |
| 6 | 既有测试 | 12 个包全绿、`vet` 0、`gofmt` clean |

## 6. 风险与缓解

| 风险 | 影响 | 缓解 |
|---|---|---|
| `ServeTLS` 对已握手 `*tls.Conn` 的兼容性假设不成立 | 无法用标准库启用 h2 | 先写 spike 测试（20 行）验证，再动生产代码；不成立则重新评估 x/net 依赖 |
| h2 开启后 WebSocket 异常 | Codespaces 等页面功能受损 | 依赖 stdlib 默认禁用 extended CONNECT；用例 3 专项回归 |
| 回源 h2 与 `tls_sni=false` 组合异常 | 核心域名不可用 | 用例 2 覆盖 `tls_sni=false` 的域名配置；`http2=false` 可一键回退 |
| 大响应在 h2 下缓冲 | 内存上涨 | 用例 4 验证流式；必要时设置 `FlushInterval` |

## 7. 里程碑

1. **M1**：`dialer` 的 ALPN 与 transport 选项 + 单元测试
2. **M2**：`mitm` 服务端 h2 + Upgrade 回退 + 端到端测试
3. **M3**：`[server] http2` 配置项、README、`v0.4.0` 发布

## 8. 验收标准

- 本地 h2 上游经 MITM 的请求，上游侧 `r.ProtoMajor == 2`
- 全部测试绿、`vet`/`gofmt` 干净、六目标交叉编译通过
- Windows 真机：浏览器 DevTools 的 Protocol 列对被接管域名显示 `h2`
- `http2 = false` 时 DevTools 显示 `http/1.1`，功能无差异
