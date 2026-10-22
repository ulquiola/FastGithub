# FastGithub Go

把 `github.com` 从"打不开"变成"能打开、还比较快"的本地代理：**干净 DNS 解析 → 多 IP 竞速 → 本地 CA 中间人**，一条命令跑起来，Windows 上还能做到浏览器零配置。

- **不做翻墙**，不加密流量，不加速游戏
- **单一静态二进制**，零运行时依赖，不再需要外部 `dnscrypt-proxy` 进程
- 用 Go 重写自 `dotnetcore/FastGithub`（上游已消失），行为对等、配置语义兼容

当前版本 **v0.4.1**。原始 .NET 源码（850 个提交，`f5425ec`）归档在 `gitee.com/angstrom/FastGithub` 的 `master` 分支，本仓库不含任何 .NET 代码。

---

## 快速开始（Windows）

1. 解压发布包，双击 **`fastgithub-tray.exe`**（GUI 子系统，没有黑框；会请求一次管理员权限）
2. 托盘出现图标后，左键打开 Web UI → “证书信任” → **安装 CA**
3. 直接访问 `github.com` 即可，浏览器无需任何代理设置

命令行等价写法：

```console
$ fastgithub run          # 显式代理 127.0.0.1:38457 + Web UI 127.0.0.1:38470
$ fastgithub install-cert # 只装证书，不改代理
$ fastgithub version
```

macOS / Linux 没有透明拦截，需要让浏览器走 `127.0.0.1:38457`，或使用 Web UI 的“系统代理”卡片（仅 macOS）、PAC 地址 `http://127.0.0.1:38457/`。

## 工作原理

```
浏览器 ──DNS──▶ 本地 53 端口（WinDivert 劫持）──▶ 干净 DoH/TCP DNS ──▶ 真实 IP
   │
   └─443/80/22/9418──▶ 本机入口 ──┬─ 命中域名：本地 CA 签叶证书，MITM 后按规则改写 SNI/Host 再连最优 IP
                                 └─ 未命中：盲隧道，端到端 TLS 不碰
```

- **显式代理**（全平台）：`127.0.0.1:38457`，`CONNECT` 命中规则走 MITM，未命中走盲隧道；根路径返回 PAC
- **透明拦截**（仅 Windows，WinDivert）：劫持 UDP 53 与 TCP 443/80/22/9418，浏览器零配置；443 被占用时自动顺延端口并由 WinDivert 重定向
- 域名规则只决定“哪些域名被接管/优化”，不决定“是否走代理”，默认 55 条来自原版 `appsettings.*.json`，在 `conf.d/`

## 常用命令

| 命令 | 作用 |
|---|---|
| `fastgithub`（无参数） | 启动托盘（等价 `fastgithub tray`，需要管理员） |
| `fastgithub run` | 前台运行显式代理 + Web UI |
| `fastgithub config validate [--path 文件]` | 校验配置，打印规则数与端口 |
| `fastgithub install-cert` / `uninstall-cert` | 安装 / 移除 CA（Windows 管理员，Linux root） |
| `fastgithub install` / `start` / `stop` / `uninstall` | 系统服务模式 |
| `fastgithub windivert-selftest` | 打印 WinDivert 路径与 DNS 自检 |
| `fastgithub version` | 版本、提交、构建时间 |

## 配置

主配置 `fastgithub.toml`，域名规则在 `conf.d/*.toml`（按文件名排序合并）。

| 键 | 默认 | 说明 |
|---|---|---|
| `server.proxy_port` | `38457` | 显式代理端口 |
| `server.ui_port` | `38470` | Web UI 端口（只监听回环） |
| `server.http2` | `true` | 被接管域名使用 HTTP/2，见下节 |
| `server.singleton` | `true` | 阻止多实例（Unix `flock`，Windows 命名互斥量） |
| `server.transparent.*` | `443/80/22/9418` | 透明入口偏好端口，被占用时自动顺延 |
| `dns.doh` | `doh.pub`、`alidns.com` | DoH 端点，失败回退 TCP 53 |
| `intercept.mode` | `auto` | `auto` / `off`；`off` 时只保留显式代理 |
| `log.level` / `log.file` | `info` / `logs/fastgithub.log` | 请求日志 |

## HTTP/2

被接管域名默认在“浏览器 ↔ 代理 ↔ 上游”两段都跑 h2，客户端不支持时自动回退 HTTP/1.1。协议升级（WebSocket）固定走独立的 h1.1 出口，101 之后双向透传。

```toml
[server]
http2 = false   # 全部回落到 HTTP/1.1，用于排查个别站点在 h2 下的异常
```

串行单请求 h2 有固定开销，8 并发时比 h1 快约 10%——数据见 [docs/benchmarks-2026-10-21.md](docs/benchmarks-2026-10-21.md)。

## Web UI

`http://127.0.0.1:38470/` 提供状态、域名命中、实时日志（SSE）、证书安装/卸载、CA 下载、系统代理开关（macOS）。**会修改状态**的接口需要启动目录下 `ui.token` 里的令牌（文件权限 0600）。

## macOS 菜单栏应用

```console
$ make macapp-install
$ open ~/Applications/FastGithub.app
```

- 无控制台窗口、无 Dock 图标（`LSUIElement`），菜单栏常驻；
- 菜单提供：代理状态、打开 Web 界面、系统代理开关、安装/卸载 CA、开机启动、退出；
- 数据在 `~/Library/Application Support/FastGithub`，升级或重建 `.app` 不会更换 CA 和 `ui.token`；
- 首次使用请在菜单里点“安装 CA”；旧 CLI 的 CA 可在钥匙串中手动删除；
- 实测物理内存约 13MB（Activity Monitor 的“内存”）；RSS 约 35MB 是因为包含共享库，不代表实际占用。

菜单栏二进制用 cgo/AppKit 构建，因此只在 macOS 上编译；CLI `fastgithub` 仍保持 `CGO_ENABLED=0` 和六目标交叉编译。

## 透明拦截的前置条件（Windows）

WinDivert 不随包内嵌，需要把 `WinDivert.dll` 与 `WinDivert64.sys` 放进 `intercept.driver_dir`、可执行文件同目录或 `windivert/`。DLL 缺失、非管理员或 `WinDivertOpen` 失败时只告警，显式代理继续可用。WinDivert 2.2.2 只有 x86/x64 驱动，`windows/arm64` 包不含它；再分发请保留其 LICENSE（LGPLv3/GPLv2）。

## 平台支持

| 平台 | 显式代理 | 透明拦截 | 托盘 | 系统代理一键切换 |
|---|---|---|---|---|
| Windows amd64 | ✅ | ✅（WinDivert） | ✅ | — |
| Windows arm64 | ✅ | —（无驱动） | ✅ | — |
| macOS amd64/arm64 | ✅ | — | — | ✅（`networksetup`） |
| Linux amd64/arm64 | ✅ | — | — | — |

全部 `CGO_ENABLED=0` 交叉编译。

## 构建与发布

```console
$ make test
$ make ci                 # 测试 + vet + gofmt + 六目标交叉编译
$ make bench              # 配置匹配 / DNS 解析 / MITM h1 vs h2
$ make fetch-windivert    # 下载并校验 WinDivert 2.2.2 到 windivert/
$ VERSION=v0.4.1 make release
```

产物在 `dist/fastgithub-<os>-<arch>.zip`，含可执行文件、`fastgithub.toml`、`conf.d/`、README 与 LICENSE；存在 `windivert/` 时只随 `windows/amd64` 打包。Windows 上没有 make 可直接 `bash scripts/ci.sh`。

## 已知限制

- **开着透明拦截时，SSH-over-443 会断**。常见写法 `Host github.com / Hostname ssh.github.com / Port 443` 命中了 `*.github.com` 规则，443 被当作 HTTPS 接管。临时办法是让 git 走 22 端口：`git -c core.sshCommand="ssh -p 22 -o HostName=github.com" …`
- 透明拦截只覆盖 DNS 与 443/80/22/9418，不是通用 VPN；不处理 QUIC/HTTP3。
- CA 与 `ui.token` 存放在部署目录，**重建发布包会换一套**，需要重装证书、旧 UI 页面失效。已记录为 [docs/issues/2026-10-22-ca-and-token-location.md](docs/issues/2026-10-22-ca-and-token-location.md)。
- UI 的“切换拦截”按钮当前返回 501，请通过 `intercept.mode` 启停。

## 文档

- 设计：[docs/specs/2026-10-20-fastgithub-go-design.md](docs/specs/2026-10-20-fastgithub-go-design.md)
- 实现计划：[docs/plans/](docs/plans/)
- 交接与验收记录：[docs/handoff-2026-10-21.md](docs/handoff-2026-10-21.md)
- 基准：[docs/benchmarks-2026-10-21.md](docs/benchmarks-2026-10-21.md)

## 致谢与许可

MIT，保留原项目署名，见 [LICENSE](LICENSE)。感谢原作者与 `WinDivert` 项目。