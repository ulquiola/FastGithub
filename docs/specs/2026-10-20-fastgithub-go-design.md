# FastGithub Go 重写设计

- 状态：已批准（2026-10-20）
- 目标仓库：`fastgithub-go`（独立仓库，非 FastGithub 备份的一部分）
- 参考实现：本机备份 `D:\WorkSpace\CodeSpace\FastGithub`，v2.1.5，850 个提交，最后提交 2025-08-02
- 许可：MIT，保留原项目与作者署名

---

## 1. 背景

### 1.1 事实基线

| 项 | 值 |
|---|---|
| 仓库 | 上游 `github.com/dotnetcore/FastGithub` 已消失；本机为其 Gitee 镜像（`gitee.com/angstrom/FastGithub`） |
| 代码规模 | 8 个 .NET 工程，约 7.6k 行（C#/XAML/JSON），另有 44 MB 预编译 dnscrypt-proxy 二进制 |
| 提交 | 850 个，工作树干净，单 tag `v2.1.5` |
| 测试 | **0 个测试工程**；CI 的 `dotnet test` 步骤是空跑 |
| 目标框架 | 主程序 `net7.0`（2024-05 已 EOL）；UI 为 `net45` WPF |
| 关键依赖 | `Yarp.ReverseProxy 1.1.1`（2022）、`DNS 7.0.0`、`Tommy 3.1.2`、`PInvoke.AdvApi32 0.7.124`（停更）、`WindivertDotnet 1.1.2`、`Microsoft.Extensions.Hosting.* 7.0.0-rc*`（浮动预发布） |
| 本机工具链 | `.NET SDK`：无（仅 8.0 运行时）；`Go 1.26.5`；`mingw64`；全部 HTTPS 出口不可用；`github.com` 被解析到 `127.0.0.1` |

### 1.2 为什么必须换语言

三条不可逆的硬伤，任何一条都会让"复活"变成持续移植：

1. **框架已 EOL 且绑定内部 API**：`FastGithub.HttpServer/TcpMiddlewares/HttpProxyMiddleware.cs` 直接使用 Kestrel 的 `Microsoft.AspNetCore.Server.Kestrel.Core.Internal.Http.HttpParser<T>`。这是私有命名空间下的实现细节，跨大版本随时可能消失。
2. **构建不可复现**：`Directory.Build.props` 把 `net7.0` 写死，`Microsoft.Extensions.Hosting.Systemd/WindowsServices` 用 `7.0.0-rc*` 浮动预发布版本；UI 需要 .NET Framework 4.5 目标包（VS2022 已不支持）。
3. **本机根本无法构建**：没有 SDK，且 NuGet 不可达。换句话说，这个备份现在连"编译一次"都做不到。

### 1.3 项目本身值得保留

技术内核是有效的，而且很小：用干净 DNS 拿到真实 IP → 按 TCP 建连延迟排序 → 本地 CA 做 TLS 中间人 → 精确控制上游 IP、SNI 与 Host。它解决的是"DNS 污染 + 单 IP 不可达"，不是翻墙。本机 `github.com → 127.0.0.1` 正是它要治的病。

因此结论是：**代码可复活，但 .NET 形态不值得复活；重写为单二进制 Go 程序更划算。**

---

## 2. 原项目机制

### 2.1 模块

| 工程 | 职责 |
|---|---|
| `FastGithub.Configuration` | 域名通配（`*` 不跨点）、SNI 表达式、端口占用探测、配置绑定 |
| `FastGithub.DomainResolve` | dnscrypt-proxy 本地端口优先 + 明文 DNS 回退；TTL 夹逼；TCP 竞速排序；域名→IP、IP→时延双缓存 |
| `FastGithub.Http` | 自建 `SocketsHttpHandler.ConnectCallback`：逐 IP 重试、自定 SNI、容忍证书名不匹配 |
| `FastGithub.HttpServer` | 自签 CA、按域名签发证书、Kestrel TLS 中间件、CONNECT 代理、隧道、反向代理、PAC |
| `FastGithub.PacketIntercept` | Windows WinDivert：DNS 应答改写为回环、TCP 端口重映射（443/80/22/9418） |
| `FastGithub.FlowAnalyze` | 连接级上下行计数与速率 |
| `FastGithub.UI` | .NET 4.5 WPF：内嵌 LiveCharts 画流量图、UDP 收日志、内嵌浏览器看 issue |

### 2.2 关键机制

- **DNS 净化**：dnscrypt-proxy（本地端口）优先，失败回退到配置的明文 DNS；仅使用支持 TCP 的 DNS，避免 UDP 被污染；A/AAAA 并发查询。
- **测速选路**：对所有候选 IP 并发 TCP 建连，最快者置首位；结果缓存 5 分钟，故障缓存 1 分钟。
- **MITM 控制**：按域名配置决定是否发 SNI、SNI 用什么值（`@domain`/`@ipaddress`/`@random`）、是否容忍证书名不匹配、是否强制指定 IP、是否改写到别的 `Destination`、是否直接短路返回自定义响应。
- **Windows 透明性**：DNS 应答改成 `127.0.0.1`/`::1`，流量自然打到本地回环监听；若 443/80/22/9418 被占用，监听器顺延到其他端口，再由 WinDivert 把目标端口改写到实际端口。

---

## 3. 目标与非目标

### 3.1 目标

- **G1 行为对等**：原版能加速的场景，Go 版同样能加速；域名/SNI/证书/改写语义逐项对齐（见 §8）。
- **G2 单二进制**：`CGO_ENABLED=0`，三平台 × 两架构交叉编译，无运行时、无外部进程依赖（不再需要 dnscrypt-proxy）。
- **G3 配置语义兼容**：TOML 承载原 `appsettings.json` 的全部字段语义，随包提供拆分配置片段。
- **G4 内置 UI**：Web UI 取代 WPF，跨平台，资源全部内嵌，离线可用。
- **G5 可测**：核心包有单元测试，回环端到端测试不依赖外网。
- **G6 更安全**：不写全局 `git http.sslverify=false`；CA 私钥文件权限收紧。

### 3.2 非目标

- N1 不做 VPN/翻墙，不加密流量，不代理游戏。
- N2 不支持已停止支持的操作系统（Windows 7 等）。
- N3 不保留 WPF UI、不保留 UDP 日志接收、不保留 `ParentProcessId` 联动。
- N4 不内置 WinDivert 二进制（见 §7.8）。
- N5 不做 dnscrypt 协议实现（用 DoH/DoT 取代）。

---

## 4. 约束

| 编号 | 约束 | 影响 |
|---|---|---|
| C1 | 本机无外网，Go 模块只能来自缓存 | 依赖限定为**标准库 + `golang.org/x/sys`**（已验证离线可编译）；TOML 解析器自研 |
| C2 | 无 WinDivert 二进制、无驱动级 E2E 环境 | 拦截子系统只能做静态验证与纯函数测试，真实收发列入手工验收 |
| C3 | 沙箱只允许写工作区 | 独立仓库先落在 `FastGithub/fastgithub-go/`，父仓 `.git/info/exclude` 忽略 |
| C4 | 本机 Go 1.26.5 | `go.mod` 声明 `go 1.22`，只用 1.22 及以前的 stdlib API |
| C5 | 系统代理/证书安装需要管理员权限 | 非管理员时降级为显式代理模式并告警，不中断 |

---

## 5. 架构

### 5.1 包边界

依赖方向单向，无环：

```
cmd/fastgithub ──► internal/app
                      ├─► internal/config
                      ├─► internal/resolver
                      ├─► internal/dialer    (依赖 resolver + config)
                      ├─► internal/certs
                      ├─► internal/mitm      (依赖 dialer + certs + config + flow)
                      ├─► internal/intercept (依赖 config + resolver)
                      ├─► internal/flow
                      └─► internal/ui        (依赖 flow + app 状态快照)
```

| 包 | 职责 | 不做什么 |
|---|---|---|
| `config` | TOML 子集解析、域名规则、SNI 表达式、热重载、校验 | 不碰网络 |
| `resolver` | DoH/DoT/明文 DNS 链、报文编解码、TTL 夹逼、IP 竞速、缓存 | 不建 TLS、不签证书 |
| `dialer` | 唯一上游出口：选 IP、定 SNI、校验证书、超时 | 不解析 HTTP |
| `certs` | CA 生成/持久化、叶子证书签发与缓存、系统信任库安装 | 不做 TLS 握手 |
| `mitm` | CONNECT 代理、隧道、TLS 终结、反向代理、PAC、Host 改写 | 不做平台劫持 |
| `intercept` | Windows WinDivert：DNS 应答、TCP 端口重映射 | 不解析 HTTP |
| `flow` | 连接级计数、速率、订阅 | 不渲染 |
| `ui` | 内嵌页面、REST、SSE、兼容端点 | 不做业务逻辑 |
| `app` | 装配、监听器编排、子命令、单实例、服务安装、优雅退出 | 不实现协议 |

### 5.2 进程模型

单进程，子命令：

| 命令 | 行为 |
|---|---|
| `fastgithub run` | 前台运行（默认） |
| `fastgithub start` | 安装并启动系统服务（Windows SCM / systemd / launchd） |
| `fastgithub stop` | 停止并卸载系统服务 |
| `fastgithub install-cert` | 仅安装并信任 CA |
| `fastgithub config validate` | 解析并校验配置后退出（不监听） |
| `fastgithub windivert-selftest` | 打开驱动、收发一个回环包、打印地址结构字段 |
| `fastgithub version` | 打印版本（ldflags 注入） |

启动顺序：加载配置 → 初始化 CA → 绑定监听器 → 启动拦截 → 启动 UI → 打印横幅。退出反向进行，总超时 1s（与原版 `UseShutdownTimeout(1s)` 一致）。

单实例：Windows 命名互斥量 `Global\FastGithub`；Unix 对配置目录下 `fastgithub.lock` 加 `flock`。

### 5.3 入口拓扑

| 入口 | Windows | Linux/macOS | 说明 |
|---|---|---|---|
| 38457 显式代理（CONNECT + 绝对 URI） | ✅ 新增 | ✅ | 偏离 1：Windows 也开，作为拦截失败兜底 |
| 80 反向代理（回环） | ✅ | ❌ | 端口占用则顺延，由拦截层重映射 |
| 443 反向代理（TLS 终结） | ✅ | ❌ | 同上 |
| 22 / 9418 转发 | ✅ | ❌ | ssh / git 协议 |
| DNS 劫持（A→127.0.0.1，AAAA→::1） | ✅ WinDivert | ❌ | 仅命中规则域名 |
| 端口重映射 | ✅ WinDivert | ❌ | 443/80/22/9418 → 实际端口 |
| PAC | ✅ | ✅ | `GET http://127.0.0.1:38457/` 返回脚本 |
| Web UI / API | ✅ | ✅ | 独立端口 38470 |

端点原样保留在 38457 与 80 上（`/flowStatistics`），供脚本与旧习惯使用。

### 5.4 数据流

1. **显式 CONNECT**：读请求行与头（上限 8KB）→ 匹配域名则回 `200` 后做 MITM，不匹配则盲隧道转发（隧道目标也走 `dialer` 的 IP 选择）。
2. **透明 443**：连接建立即 `tls.Server` 握手，从 ClientHello 的 SNI 取域名；无 SNI 时用默认证书，后续在 HTTP 层按 `Host` 兜底。
3. **80 反向代理**：按 `Host` 查配置；`Response` 命中则短路响应，`Destination` 命中则改写目标前缀，否则原样转发。
4. **上游出口**：所有出站连接（MITM 转发、隧道、反向代理）都必须经过 `dialer`，禁止直接 `net.Dial`。

### 5.5 关键设计决策

| 编号 | 决策 | 理由 |
|---|---|---|
| D1 | `dialer` 是唯一上游出口 | 原版把 SNI/证书逻辑分散在 `HttpClientHandler` 与 `TunnelMiddleware`，语义不一致；收敛后可单测、可审计 |
| D2 | 平台差异只存在于 `intercept` 与 3 个 `trust_*` 文件，用构建标签隔离 | 其余代码保持平台无关，测试面最大化 |
| D3 | UI 独立端口 38470 | 38457 的普通 GET 必须返回 PAC（兼容原版），不能同时承载 UI |
| D4 | 不做全局 `git sslverify=false` | 原版这行对全网关闭证书校验；改为 `git config --global http.<url>.sslCAInfo <CA 路径>` 定点生效 |
| D5 | 证书校验不用 `InsecureSkipVerify` 裸奔 | 用 `VerifyConnection` 复刻原版 SAN 判定，保持"容忍不匹配"只对配置过的域名生效 |
| D6 | WinDivert 通过 `syscall` 调用，不引入 cgo | 保持 `CGO_ENABLED=0`，交叉编译无需 mingw |

### 5.6 错误处理与降级

| 场景 | 行为 |
|---|---|
| 配置文件语法错误 / 未知键 | 启动即失败，打印行号与列号；运行期热重载失败则保留旧配置并告警 |
| 端口被占用（显式代理 / UI） | 启动失败并提示改 `server.proxy_port` / `server.ui_port` |
| 透明端口被占用 | 顺延到下一个空闲端口，由拦截层重映射（不失败） |
| 所有 DNS 源失败 | 返回 `502` 并在响应体给出尝试过的解析器；不缓存空结果超过 30s |
| 上游全部 IP 连接失败 | 逐个尝试后返回错误，错误体内含每个 IP 的失败原因 |
| CA 安装失败 | 告警并输出手动安装指引，不中断服务 |
| WinDivert 缺失 / 非管理员 / 打开失败 | 告警并降级为显式代理模式，其余功能正常 |
| UI 变更接口令牌错误 | `401`，不泄露内部状态 |

---

## 6. 配置模型

### 6.1 加载顺序

内置默认值 → `fastgithub.toml` → `conf.d/*.toml`（按文件名升序，后者覆盖前者）。对应原版 `appsettings.json` + `appsettings/*.json` 的加载顺序。

热重载：2s 间隔轮询 `mtime`，变化后整体重新解析，原子替换配置对象；解析失败保留旧配置并告警（绝不半应用）。

### 6.2 Schema

```toml
[server]
proxy_port = 38457
ui_port    = 38470
singleton  = true

[server.transparent]   # 仅 Windows 生效
https = 443
http  = 80
ssh   = 22
git   = 9418

[dns]
doh      = ["https://doh.pub/dns-query", "https://dns.alidns.com/dns-query"]
fallback = ["8.8.8.8:53", "119.29.29.29:53", "114.114.114.114:53"]
timeout  = "4s"

[intercept]
mode       = "auto"    # auto | off
driver_dir = ""

[log]
level = "info"
file  = "logs/fastgithub.log"

[domains."github.com"]
tls_sni = false

[domains."*.githubusercontent.com"]
tls_ignore_name_mismatch = true

[domains."fonts.googleapis.com"]
destination = "https://fonts.geekzu.org/"

[domains."demo.fastgithub.com".response]
status       = 404
content_type = "text/plain;charset=utf-8"
content      = "这是一个用于示范配置的域名"
```

### 6.3 字段映射

| 原 JSON | TOML | 默认值 | 语义 |
|---|---|---|---|
| `FastGithub.HttpProxyPort` | `server.proxy_port` | 38457 | 显式代理端口 |
| `FastGithub.FallbackDns` | `dns.fallback` | 见上 | 仅使用**支持 TCP** 的 DNS |
| `DomainConfigs.<pattern>` | `[domains."<pattern>"]` | — | 键为域名通配表达式 |
| `TlsSni` | `tls_sni` | `false` | `false` 即不发 SNI |
| `TlsSniPattern` | `tls_sni_pattern` | `""` | `tls_sni=true` 且为空 → `@domain` |
| `TlsIgnoreNameMismatch` | `tls_ignore_name_mismatch` | `false` | 证书名不匹配时是否放行 |
| `Timeout` | `timeout` | 无 | Go duration 字符串 |
| `IPAddress` | `ip` | 无 | 强制上游 IP |
| `Destination` | `destination` | 无 | 相对或绝对 URI |
| `Response.StatusCode` | `response.status` | — | 短路响应 |
| `Response.ContentType` | `response.content_type` | — | |
| `Response.ContentValue` | `response.content` | — | |
| `Serilog.*` | `[log]` | `info` | 仅保留级别与文件 |

### 6.4 解析器

自研 TOML 子集，只支持：`[table]`、`[table."quoted.key"]`、`key = value`（字符串、整数、布尔、字符串数组、内联表不支持）、`#` 注释、基本转义。**未知键报错**而非忽略，配合 `config validate` 把拼写错误挡在启动前。同一文件内重复表头（如两次 `[a]`）会合并到同一张表，解析器不报错；这是 `conf.d` 片段逐文件解析的宽松行为。

随包片段：`conf.d/10-github.toml`、`20-google.toml`、`30-microsoft.toml`、`40-packages.toml`、`50-misc.toml`，逐条转写原版 9 份 `appsettings.*.json`，不新增策略。

---

## 7. 子系统

### 7.1 resolver

- 查询链：DoH（配置顺序，传输/响应失败的端点冷却 30s 后重试）→ 明文 DNS（仅 TCP 可达者）。DoH 用 `net/http` 手写 `application/dns-message` 请求，不引入 DNS 库。
- 超时：`dns.timeout` 是整次解析的总预算（A/AAAA 并发共享），剩余预算按剩余源数量均分；调用方取消或预算耗尽时不写负缓存。
- 报文：自实现 DNS 编解码（question/answer、A/AAAA、压缩指针只读不写），约 300 行。
- 缓存：`domain→[]IP`（TTL 夹逼 30s~10min，CNAME 链取整链最小 TTL）、`IP→latency`（正常 5min / 故障 1min）。并发同键请求用 `singleflight`（自实现，约 40 行）。
- 竞速：对候选 IP 并发 TCP 拨号（2s 上限，最多探测 32 个），最快者置首位；全部失败则按原序返回。
- 负缓存：非 socket 错误 30s，`*net.OpError` 10min（对齐原版 `minTimeToLive`/`maxTimeToLive`）。

### 7.2 dialer

```go
type Target struct {
    Host string
    Port int
    Conf config.DomainConfig
}

func (d *Dialer) DialTCP(ctx context.Context, t Target) (net.Conn, error)
func (d *Dialer) DialTLS(ctx context.Context, t Target) (*tls.Conn, error)
```

- IP 顺序：`conf.IP` 优先 → resolver 结果（竞速排序）。
- SNI：`conf.SNIPattern()` 展开 `@domain`/`@ipaddress`/`@random`，结果为空则不发 SNI。
  - 注意：当 SNI 值为 IP 字面量时，Go 与 .NET 都**不会**在 ClientHello 中发送 SNI（RFC 6066 禁止）。因此 `@ipaddress` 的实际效果等于不发 SNI，该等价性写入测试用例。
- 证书校验：`InsecureSkipVerify=true` + 自定义 `VerifyConnection`（同时覆盖 TLS 1.3 会话恢复）：
  - 链验证（系统根 + 可选本机 CA 池）；
  - 名称校验：手写 SAN 匹配 `dnsName == domain || (dnsName[0]=='*' && strings.HasSuffix(domain, dnsName[1:]))`，**不复用** `VerifyHostname`（其 IP SAN 与通配规则和原版不同）；
  - `tls_ignore_name_mismatch=true` 时跳过名称校验，链校验仍执行。
- 超时：建连 10s、TLS 握手 10s（对齐原版，域名 `timeout` 可覆盖）；连接建立后受请求上下文控制。
- 无域名规则时（如 `Destination` 改写后的镜像主机）默认发送真实主机名作 SNI。

### 7.3 certs

- CA：RSA 2048，`CN=FastGithub`，有效期 10 年，`BasicConstraints`（critical，CA=true，pathLen=1）、`KeyUsage`（digitalSignature|CrlSign|KeyCertSign）、`EKU`（serverAuth+clientAuth）、`SKI`、`SAN`（CN 值）。
- 叶子：RSA 2048，`CN=<domain>`，有效期 1 年（不超过 CA），`AKI` 取自 CA 的 `SKI`，`SAN` = 域名 + 机器名 + `127.0.0.1` + `::1`，`EKU` 同 CA。
- 存储：`cacert/fastgithub.key`（PKCS#1 PEM，权限 0600）、`cacert/fastgithub.crt`（0700 目录）；兼容读取旧版的 `.cer`。
- 信任安装与卸载：
  - Windows：`certutil -addstore -f Root <crt>`；卸载用 `certutil -delstore Root <SHA1 指纹>`；
  - Linux：复制到 `/usr/local/share/ca-certificates/fastgithub.crt` 后 `update-ca-certificates`；卸载删除该文件后再次执行 `update-ca-certificates`；
  - macOS：`security add-trusted-cert -d -r trustRoot -k /Library/Keychains/System.keychain`；卸载用 `security delete-certificate -Z <SHA1 指纹> -t /Library/Keychains/System.keychain`（按指纹精确删除）。
- 叶子缓存：`map[string]*tls.Certificate` + 过期清理；同一域名复用，避免每连接重签。

### 7.4 mitm

- **CONNECT**：`bufio.Reader` 读到 `\r\n\r\n` 为止（上限 8KB，按行有界读取），解析 `CONNECT host:port`；回 `HTTP/1.1 200 Connection Established\r\n\r\n`。首字节与 CONNECT 解析共 10s 读超时，预读字节用 `bufferedConn` 保留。
- **MITM**：`tls.Server(conn, cfg)`，`GetCertificate` 按 SNI 取叶子证书；握手 10s 超时；握手后把该连接交给一次性 `http.Server`（`singleConnListener`，`IdleTimeout=5min`）处理，handler 为反向代理。
- **隧道**：不匹配域名则 `dialer.DialTCP` 后双向复制（32KB 池化），读/写都有 5min 空闲超时。
- **反向代理**：手写反向代理（语义等价 `httputil.ReverseProxy`）+ 自定义 `Transport`（`DialContext`/`DialTLSContext` 走 `dialer`），保留原始 `Host` 与 `%2F` 等路径编码；`Destination` 改写用 `url.Parse` + 路径拼接，语义等价 `new Uri(baseUri, destination)`。
- **PAC**：文本与原版同构，`PROXY` 值取请求头里的 `Host`（缺省 `127.0.0.1:38457`），规则逐条 `shExpMatch`：

```
function FindProxyForURL(url, host){
    var fastgithub = 'PROXY <请求 Host>';
    if (shExpMatch(host, 'github.com')) return fastgithub;
    ...
    return 'DIRECT';
}
```

- **请求日志**：记录域名、方法、状态、耗时、上下行字节；写环形缓冲（供 SSE）与文件。

### 7.5 intercept（Windows）

- **加载**：按 `intercept.driver_dir` → exe 同目录 → `windivert\` → `%PATH%` 找 `WinDivert.dll`，`WinDivert64.sys` 必须同目录；`syscall.NewLazyDLL` 取符号：`WinDivertOpen/Recv/Send/Close/HelperCalcChecksums`。
- **句柄**：① `udp.DstPort == 53`；② `loopback and (tcp.DstPort == 443/80/22/9418 or tcp.SrcPort == <对应实际监听端口>)`。
- **地址结构**：用 WinDivert 2.x 的 80 字节 WINDIVERT_ADDRESS（flags 在 offset 8，union 在 16）；`Layer`/`Outbound`/`Loopback`/`Impostor` 等按 `windivert.h` 偏移定义；启动时 `windivert-selftest` 打印解析结果供人工核对。**这是本设计唯一无法在本机验证的部分**（见 §10）。
- **DNS 应答**：解析 QNAME/QTYPE，命中规则则构造 `A 127.0.0.1` / `AAAA ::1`（TTL 300s），复制 ID 与 question，置 `QR|AA|RD|RA`，交换 IP/端口，置 `Impostor`，重算 IP/UDP 校验和；未命中放行。
- **TCP 重映射**：仅改源/目标端口与校验和，逻辑等价 `TcpInterceptor.ModifyTcpPacket`。
- **SSH/Git 转发**：22/9418 重映射到本地 listener 后固定转发到 `github.com:22` / `github.com:9418`（与原版 `GithubSshReverseProxyHandler`/`GithubGitReverseProxyHandler` 一致）。
- **IPv6 降级**：任一 `::1` 监听失败时，AAAA 查询返回 NOERROR/NODATA，促使客户端回退 IPv4。
- **降级**：非管理员 / DLL 缺失 / `Open` 失败 → 记警告，透明入口不启用，显式代理仍可用；`intercept.mode="off"` 可强制关闭。

### 7.6 flow

连接包装器统计读/写字节；1s ticker 计算速率（bytes/s）；保留最近 60 个采样点供 UI 画图；累计值供 `/flowStatistics`。

### 7.7 ui

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/` | 否 | 内嵌单页 |
| GET | `/app.js`、`/style.css` | 否 | 内嵌静态资源 |
| GET | `/api/status` | 否 | 版本、监听端口、拦截模式、CA 指纹、DNS 健康 |
| GET | `/api/domains` | 否 | 规则列表与命中计数 |
| GET | `/api/logs` | 否 | SSE 日志流（环形缓冲 1000 条） |
| GET | `/api/cert` | 否 | 下载 CA |
| POST | `/api/cert/install` | 是 | 安装并信任 CA |
| POST | `/api/cert/uninstall` | 是 | 从系统信任库移除 CA |
| POST | `/api/intercept` | 是 | 预留（当前 501，后续实现运行时开关） |
| GET | `/flowStatistics` | 否 | 兼容端点，字段 `TotalRead/TotalWrite/ReadRate/WriteRate`（PascalCase）；同时注册在 38457 与 Windows 的 80 上 |

鉴权：变更类接口要求 `X-FastGithub-Token`，令牌启动时随机生成、写入 CA 目录同级 `ui.token`（0600），页面由服务端注入。

### 7.8 服务与打包

- `start/stop`：Windows 用 SCM（`CreateService` 指向自身 `run`；`main` 调用 `StartServiceCtrlDispatcher` 注册控制处理器，失败回滚删除）；Linux 写 `/etc/systemd/system/fastgithub.service`（`Type=simple`、`Restart=always`）；macOS 写 launchd plist。
- 构建矩阵：`CGO_ENABLED=0` × `{windows,linux,darwin}` × `{amd64,arm64}`，`-ldflags "-s -w -X fastgithub-go/internal/app.version=... -X ...commit=... -X ...buildTime=..."`。
- WinDivert 不内嵌：`make fetch-windivert` 下载官方包并校验 SHA-256，放入发布 zip；仓库只保留脚本与校验和。
- 发布包结构：`fastgithub(.exe)`、`fastgithub.toml`、`conf.d/`、`windivert/`（可选）、`README.md`、`LICENSE`。

---

## 8. 行为对齐表

每行都要有"Go 实现位置 + 测试"两列，M3 时逐项核对。

| 原实现 | Go 实现 | 对齐要点 | 测试 |
|---|---|---|---|
| `DomainPattern.cs` | `internal/config/pattern.go` | `*`→`[^.]*`、大小写不敏感、锚定、排序比较器 | `pattern_test.go` |
| `TlsSniPattern.cs` | `internal/config/sni.go` | 三个变量替换、IP 值不发 SNI | `sni_test.go` |
| `DomainConfig.GetTlsSniPattern` | `config.Domain.SNIPattern()` | `tls_sni=false` → 空；`true`+空表达式 → `@domain` | `sni_test.go` |
| `FastGithubConfig.cs` | `internal/config/store.go` | 排序查找、缓存（上限 4096 条）、热更新整体替换 | `store_test.go` |
| `GlobalListener.cs` | `internal/app/ports.go` | 从偏好端口向上找空闲；TCP/UDP 都判 | `ports_test.go` |
| `DnsClient.cs` | `internal/resolver/*` | TTL 夹逼、负缓存时长、A/AAAA、并发去重 | `resolve_test.go` |
| `DnscryptProxy.cs` | 删除（内置 DoH） | 不再拉起外部进程 | `doh_test.go` |
| `IPAddressService.cs` | `internal/resolver/race.go` | 竞速排序、2s 探测、故障 1min | `race_test.go` |
| `DomainResolver.cs` | `internal/resolver/resolve.go` | 组装、缓存键 | `resolve_test.go` |
| `HttpClientHandler.cs` | `internal/dialer/dialer.go` | IP 顺序、SNI、证书判定、10s 超时 | `dialer_test.go` |
| `LifetimeHttpHandler*.cs` | `internal/dialer/pool.go` | 连接复用与失效清理 | `pool_test.go` |
| `KestrelServerExtensions.cs` | `internal/app/run.go` | 端口、TLS 条件、日志文案 | `run_test.go` |
| `TlsInvadeMiddleware.cs` | `internal/mitm/sniff.go` | 首两字节 `0x16 0x03` 判定 | `tunnel_test.go` |
| `HttpProxyMiddleware.cs` | `internal/mitm/connect.go` | 请求行/头解析、`200/400` 响应 | `connect_test.go` |
| `TunnelMiddleware.cs` | `internal/mitm/tunnel.go` | 双向复制、目标选择 | `tunnel_test.go` |
| `HttpReverseProxyMiddleware.cs` | `internal/mitm/reverse.go` | `Host`、`Destination`、`Response` | `reverse_test.go` |
| `HttpProxyPacMiddleware.cs` | `internal/mitm/pac.go` | 文本逐字一致 | `pac_test.go` |
| `CertGenerator.cs` / `CertService.cs` | `internal/certs/ca.go`、`leaf.go` | 扩展项、SAN、有效期、缓存 | `ca_test.go`、`leaf_test.go` |
| `CaCertInstallerOf*.cs` | `internal/certs/trust.go` | 三个平台命令 | `trust_test.go` + 手工验收 |
| `TcpInterceptor.cs` / `DnsInterceptor.cs` | `internal/intercept/*` | 端口改写、DNS 应答、校验和 | `packet_test.go`、`dnsanswer_test.go` + 手工 |
| `FlowAnalyzer.cs` | `internal/flow/flow.go` | 计数与速率 | `flow_test.go` |
| `FastGithub.UI` | `internal/ui/*` | 流量图、日志、证书、流量统计 | `ui_test.go` |
| `appsettings*.json` | `conf.d/*.toml` | 逐条转写 | golden 测试 |

---

## 9. 测试策略

### 9.1 单元测试（纯函数优先）

| 对象 | 断言 |
|---|---|
| TOML 解析 | 注释、引号键、数组、时长、错误定位；9 份原配置转写后的 golden 对比 |
| `DomainPattern` | `*` 不跨点、大小写、锚定；排序结果与 C# 逐项一致 |
| `TlsSniPattern` | 变量替换、IP 值不发 SNI |
| `Destination` | 相对/绝对拼接、路径与查询串保留 |
| 证书 | 扩展项、链式验证、SAN、有效期、序列号随机性 |
| DNS 报文 | 编解码、A/AAAA、TTL、IP/TCP/UDP 校验和逐字节 golden |
| resolver | `httptest` 假 DoH、TTL 夹逼、负缓存、回退顺序 |
| dialer | 假 TLS 服务器记录 SNI、证书不匹配两路径、IP 竞速 |

### 9.2 回环集成测试

用真实 `app` 装配，配置里 `ip` 指到 `127.0.0.1:<随机端口>` 的假上游：

- CONNECT → MITM → 证书由本机 CA 签发、`Host` 保留、正文完整；
- 透明 443（直连本地监听器，模拟 SNI）→ 路由命中；
- `Destination` 改写 → 假上游收到重写后的路径；
- PAC 文本逐字比对；
- `/flowStatistics` 字段名与类型；
- UI 首页 200、令牌校验、SSE 收到日志。

### 9.3 构建验证

`go vet ./...`、`gofmt -l`、`go test ./...`、6 个 `GOOS/GOARCH` 组合的 `go build`（离线，`GOPROXY=off`）。

竞态检测 `go test -race` 需要 cgo：本机有 mingw 可跑；无 gcc 的 CI 上只在 Linux 跑。

测试全程 `GOCACHE` 指向仓库内 `.gocache/`（沙箱默认缓存目录不可写），该目录已加入 `.gitignore`。

### 9.4 手工验收（需有网/管理员）

1. Windows：放置 WinDivert → 管理员运行 → `nslookup github.com` 应返回 `127.0.0.1` → `curl -v https://github.com` 通 → 证书链显示本机 CA。
2. 浏览器信任 CA 后打开 GitHub，头像与 release 下载正常。
3. `git clone https://github.com/...` 与 `git@github.com:...` 均通。
4. `fastgithub start/stop` 服务安装卸载；重启后自动运行。
5. Linux：`HTTP_PROXY=http://127.0.0.1:38457 git clone ...`，`update-ca-certificates` 后无证书告警。

---

## 10. 风险

| 风险 | 后果 | 缓解 |
|---|---|---|
| WinDivert 地址结构/位域写错 | 透明拦截静默失效 | 80 字节 WINDIVERT_ADDRESS + 命名偏移；`windivert-selftest` 打印字段；`Open` 失败即降级；真实收发留手工验收 |
| QUIC/HTTP3 绕过 TCP 拦截 | 部分站点加速失效 | 文档明确边界；PAC/显式代理兜底 |
| ECH 普及后 SNI 不可见 | 无法按域名分流 | 当前未部署；`intercept`/`mitm` 解耦，判据可替换 |
| 无 SNI 客户端证书不匹配 | 连接失败 | 默认证书 + `Host` 兜底；`tls_sni=true` 强制 |
| 自研 TOML 边界 bug | 配置读错 | 子集最小化、未知键报错、`config validate`、golden 测试 |
| CA 私钥明文 | 本机提权面 | 0600/0700、文档警示、不改全局 git 校验 |
| Go 与 .NET 证书校验差异 | 行为偏差 | 自定义 `VerifyConnection`、手写 SAN 匹配、对照测试 |
| 大文件吞吐不如 Kestrel | clone 体验差 | 32KB 池化缓冲、关闭 `TCP_NODELAY` 批量发送、基准测试 |
| WinDivert LGPL 与驱动签名 | 分发受阻 | 不内嵌、校验官方 SHA-256、随包附许可证 |

---

## 11. 里程碑

| | 内容 | 退出条件 |
|---|---|---|
| **M1** 内核切片 | `config`/`resolver`/`dialer`/`certs`/`mitm`/`flow` + 38457 代理 + PAC + UI 骨架 | `go test ./...` 全绿；CONNECT→假上游端到端通过；6 目标交叉编译成功 |
| **M2** Windows 切片 | `intercept` + 透明 80/443/22/9418 + 服务安装 + 提权与降级 | 单测与自检通过；有网 Windows 按 §9.4 跑通（人工） |
| **M3** 收尾 | `conf.d` 转写、`/flowStatistics` 兼容、对齐表全项、CI/发布脚本、README | 对齐表每行有实现位置与测试；发布包可产出 |

单个计划承载不了三个里程碑：**先为 M1 出实现计划**，M1 验收通过后再为 M2、M3 各自出计划。每个计划自带测试与验收步骤。

---

## 12. 目录结构

```
fastgithub-go/
├── cmd/fastgithub/main.go
├── internal/
│   ├── app/          app.go listeners.go ports.go service_windows.go service_linux.go service_darwin.go singleton.go
│   ├── config/       toml.go schema.go pattern.go sni.go store.go validate.go
│   ├── resolver/     dnsmsg.go doh.go resolve.go race.go cache.go singleflight.go
│   ├── dialer/       dialer.go tlsverify.go pool.go
│   ├── certs/        ca.go leaf.go store.go trust.go
│   ├── mitm/         proxy.go connect.go tunnel.go reverse.go pac.go sniff.go
│   ├── intercept/    intercept.go windivert_windows.go address.go dnsanswer.go packet.go
│   ├── flow/         flow.go
│   └── ui/           server.go sse.go assets/index.html assets/app.js assets/style.css
├── conf.d/
├── docs/specs/
├── testdata/
├── Makefile
├── go.mod
├── go.sum
├── LICENSE
└── README.md
```

---

## 13. 未决与后续

- macOS 的信任安装与拦截（无平台后端）只做到"代理可用"，不承诺透明。
- IPv6 上游：原版按 `OSSupportsIPv6` 决定是否查 AAAA；Go 版默认查，可配置关闭。
- 是否在内置 DoH 之外保留"外挂 dnscrypt-proxy"兼容开关：暂不做，配置里出现 `dnscrypt` 键会报错。
- 性能基准（并发连接数、单连接吞吐）在 M3 补 `go test -bench`。
- **`@ipaddress` 展开为 IP 字面量作 SNI 的语义（M1-D 已确认）**：Go 与 .NET 对 IP 字面量都不发送 SNI（RFC 6066），实际等价于"不发 SNI"。`dialer_test.go` 用假 TLS 服务器断言 `@ipaddress` 的 `ClientHello.ServerName` 为空；若将来确需发送 IP 字面量 SNI，标准库不支持，需自定义 ClientHello，成本较高。
