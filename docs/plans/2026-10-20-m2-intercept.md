# M2 Windows 透明拦截与服务安装实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 Windows 上用 WinDivert 把命中域名的 DNS A/AAAA 改写为 127.0.0.1/::1，并把 443/80 的环回连接重映射到本地监听器；提供 `windivert-selftest` 与服务安装/启停命令；非 Windows 或失败时降级为显式代理。

**Architecture:** `internal/intercept` 分离纯逻辑（80 字节 `WINDIVERT_ADDRESS` 位域、IPv4/IPv6+UDP/TCP 解析、DNS 查询/应答、校验和）与 Windows 系统调用层（`syscall.NewLazyDLL`，构建标签）；`internal/app` 用端口顺延监听 443/80，Windows 上启动两个 WinDivert 句柄（DNS 与 TCP 重映射），失败只告警并保留 38457。

**Tech Stack:** Go 1.22，仅标准库（`syscall`/`unsafe`/`encoding/binary`/`net`）；Windows 专用文件用 `//go:build windows`；`CGO_ENABLED=0`。

**Spec:** `docs/specs/2026-10-20-fastgithub-go-design.md` §7.5（intercept）、§7.8（服务与打包）、§5.3（入口拓扑）、§9.4（手工验收）、§10（风险）。

## Global Constraints

- 不改 `go.mod` 依赖；Windows 调用只用 `syscall.NewLazyDLL`/`SyscallN`。
- 非 Windows 上 `intercept.Start` 返回 `ErrUnsupported`，应用继续提供 38457。
- WinDivert 缺失/非管理员/Open 失败 → 告警并降级；`intercept.mode="off"` 强制关闭。
- DNS 应答：A `127.0.0.1`、AAAA `::1`、TTL 300、`QR|AA|RD|RA`、复制 ID/question、置 Impostor。
- 纯逻辑必须在 macOS 上单测；Windows 文件只做交叉编译验证。
- 每个任务结束 `go test ./...` 全绿、`go vet`/`gofmt` 无输出。

---

## 文件结构

| 文件 | 职责 |
|---|---|
| `internal/intercept/address.go` | 80 字节地址位域解析/修改 |
| `internal/intercept/packet.go` | IPv4/IPv6、UDP/TCP 解析、端口改写、校验和 |
| `internal/intercept/dnsanswer.go` | DNS 查询解析与 A/AAAA 应答 |
| `internal/intercept/intercept_windows.go` | WinDivert 加载、两个句柄、收发循环 |
| `internal/intercept/intercept_other.go` | 非 Windows 的 `ErrUnsupported` |
| `internal/intercept/selftest.go` | 地址/包解析自检文本 |
| `internal/app/ports.go` | 首选端口被占用时顺延 |
| `internal/app/listeners.go` | 443/80 透明监听（Windows） |
| `internal/app/service_*.go` | Windows SCM / systemd / launchd |
| `cmd/fastgithub/main.go` | `windivert-selftest`、`install/uninstall/start/stop` |

---

### Task 1: 地址与包纯逻辑

**Files:** `internal/intercept/address.go`、`packet.go`、`dnsanswer.go` + 测试

**Interfaces:**
```go
const AddressSize = 64
type Address [AddressSize]byte
func (a *Address) Layer() uint8
func (a *Address) Outbound() bool
func (a *Address) SetOutbound(bool)
func (a *Address) Impostor() bool
func (a *Address) SetImpostor(bool)

type Packet struct { Raw []byte; Version int; SrcIP, DstIP net.IP; Protocol uint8; SrcPort, DstPort uint16; Payload []byte }
func ParsePacket(raw []byte) (*Packet, error)
func (p *Packet) SetDstPort(port uint16)
func (p *Packet) SetSrcPort(port uint16)
func (p *Packet) SwapAddresses()
func RebuildUDP(raw []byte, payload []byte) ([]byte, error)
func RecalculateChecksums(raw []byte) error

func DNSResponse(query []byte, store *config.Store) ([]byte, bool, error)
```
- 位域按小端解析：offset 8 的 uint32，Layer 0-7、Event 8-15、Sniffed 16、Outbound 17、Loopback 18、Impostor 19。
- IPv4 支持 IHL、total length、fragments（非首片直接放行）；IPv6 支持 40 字节头与常见扩展头（跳过 Hop-by-Hop/Routing/DestOpts，拒绝 Fragment 非首片）。
- UDP/TCP 端口偏移与伪首部校验和。
- DNS 应答只处理 QDCOUNT=1、QTYPE A/AAAA；命中 Store 才返回 `ok=true`。
- 测试：地址位域、IPv4/UDP 应答重写、IPv6/AAAA、TCP 端口改写、校验和、未命中放行、畸形包。

---

### Task 2: Windows WinDivert 层

**Files:** `internal/intercept/intercept_windows.go`、`intercept_other.go`、`selftest.go`

**Interfaces:**
```go
type Config struct {
    Store *config.Store
    Preferred map[int]int // 实际监听端口 -> 首选端口（443/80）
    DriverDir string
}
func Start(ctx context.Context, cfg Config) (*Handle, error)
func Available() bool
func SelfTestText() string
```
- `loadDLL` 按 `driver_dir` → exe 目录 → `windivert\` → PATH 查找；检查 `WinDivert64.sys` 同目录。
- DNS 过滤器 `udp.DstPort == 53`；TCP 过滤器 `loopback and (tcp.DstPort == 443 or tcp.DstPort == 80 or tcp.SrcPort == <实际端口>)`。
- DNS 循环：命中且 A/AAAA → 交换地址、置 Impostor、`Outbound=false`、发送应答；否则原包回注。
- TCP 循环：出站 443/80 → 改 DstPort 为实际端口；入站实际端口 → 改 SrcPort 为首选端口；重算校验和；其他原包回注。
- 非 Windows：`Available()=false`，`Start` 返回 `ErrUnsupported`。
- `SelfTestText` 打印一个构造地址的 Layer/Outbound/Loopback/Impostor 与 DNS 解析结果，供 `windivert-selftest`。

---

### Task 3: 端口顺延与透明监听

**Files:** `internal/app/ports.go`、`listeners.go` + 测试；`internal/mitm` 暴露 `ServeTLS`/`ServeHTTPConn`

- `ListenTCPFallback(host string, preferred int, attempts int) (net.Listener, int, error)`：从 preferred 起顺延；preferred=0 直接随机。
- 443 监听：accept 后 `mitm.Server.ServeTLS(ctx, conn)`（TLS 由 CA 签发，SNI 取域名，交给反向代理）。
- 80 监听：accept 后 `mitm.Server.ServeHTTPConn(ctx, conn)`（按 Host 反向代理）。
- 端口映射传给 `intercept.Config`；`intercept.Start` 失败只告警。
- 测试：占住首选端口后顺延；443/80 透明监听在 macOS 上可用假上游验证（用 `ServeTLS` 直接连本地监听器，模拟 SNI）。

---

### Task 4: 服务安装与 CLI

**Files:** `internal/app/service_windows.go`、`service_linux.go`、`service_darwin.go`、`service.go`、`cmd/fastgithub/main.go` + 测试

**Interfaces:**
```go
func InstallService(exePath string) error
func UninstallService() error
func StartService() error
func StopService() error
func SystemdUnit(exePath string) string   // 纯函数，测试
func LaunchdPlist(exePath string) string  // 纯函数，测试
```
- Windows：SCM `OpenSCManager`/`CreateService`（指向 `<exe> run --path <config>`，自动启动，失败删除回滚）/`DeleteService`/`StartService`/`ControlService(STOP)`；`main` 通过 `StartServiceCtrlDispatcher` + `RegisterServiceCtrlHandler` 上报 RUNNING/STOPPED，交互运行遇 1063 继续。
- Linux：写 `/etc/systemd/system/fastgithub.service`，`systemctl daemon-reload` + `enable --now` / `disable --now`。
- macOS：写 `/Library/LaunchDaemons/com.fastgithub.fastgithub.plist`，`launchctl load/unload`。
- CLI：`install`、`uninstall`、`start`、`stop`、`windivert-selftest`；非 Windows 上 `windivert-selftest` 打印不支持。
- 测试：systemd/launchd 文本包含可执行路径与 `run`；命令注入用 fake runner；Windows 文件交叉编译。

---

## 验证清单

- [ ] `go test ./...` 全绿，`go test -race ./internal/intercept ./internal/app` 通过
- [ ] `go vet`、`gofmt` 无输出
- [ ] windows/amd64、windows/arm64、linux/darwin 交叉编译
- [ ] `windivert-selftest` 在 macOS 打印“不支持”，在 Windows 打印地址字段
- [ ] 手工（需 Windows + 管理员 + WinDivert）：`nslookup github.com` 返回 127.0.0.1；`curl -v https://github.com` 证书链为本机 CA；`git@github.com` 通（22 端口为已知限制，见 handoff）
