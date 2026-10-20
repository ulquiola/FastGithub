# FastGithub Go

FastGithub 的 Go 重写版：解决 `github.com` 打不开、头像加载失败、release 下载与 `git clone/pull/push` 失败的问题。

> 上游 `github.com/dotnetcore/FastGithub` 已消失，本项目基于其 `v2.1.5` 备份源码重写，保持行为对等、配置语义兼容。

- 不做翻墙，不加密流量，不加速游戏
- 只支持仍被发行方支持的操作系统
- 单一静态二进制，无运行时依赖，不再需要外部 dnscrypt-proxy 进程

## 状态

M1 已完成；M2 代码完成（Windows WinDivert 透明拦截与服务安装），待在 Windows 上手工验收；M3 已完成。完整设计与可行性分析见 [docs/specs/2026-10-20-fastgithub-go-design.md](docs/specs/2026-10-20-fastgithub-go-design.md)。

```console
$ fastgithub config validate
配置有效: 55 条域名规则，代理端口 38457，UI 端口 38470
$ fastgithub config validate --path ./fastgithub.toml
$ fastgithub install-cert
CA 证书: cacert/fastgithub.crt
已加入系统信任库
$ fastgithub run
代理已启动: 127.0.0.1:38457
UI: http://127.0.0.1:38470/
$ fastgithub version
```

`run` 启动显式代理与 Web UI；Windows 上若 WinDivert 可用且 `intercept.mode != "off"`，还会启用 443/80/22/9418 透明入口（22/9418 固定转发到 `github.com`）：命中域名走本地 CA 的 MITM，未命中走盲隧道，`GET http://127.0.0.1:38457/` 返回 PAC，`GET /flowStatistics` 返回 `TotalRead/TotalWrite/ReadRate/WriteRate`。UI 提供状态、域名命中、实时日志（SSE）与 CA 下载；变更接口需要启动目录下 `ui.token` 中的令牌（0600）。

`install-cert` 会在 `cacert/` 生成 CA（已存在则复用），并把证书加入系统信任库；Windows 需要管理员权限，Linux/macOS 需要 root/sudo。Windows 下路径分隔符显示为 `\`。

实现按里程碑推进：

| 里程碑 | 内容 | 状态 |
|---|---|---|
| M1 | 配置 / DNS / 拨号 / 证书 / MITM / 流量统计 + 38457 代理 + PAC + Web UI 骨架 | 已完成（M1-A~E） |
| M2 | Windows WinDivert 透明拦截 + 服务安装 | 代码完成，待 Windows 手工验收 |
| M3 | 配置片段转写、兼容端点、行为对齐核对、CI 与发布脚本 | 已完成 |

## 构建与发布

```console
$ make test
$ make ci                 # 测试 + vet + gofmt + 六目标交叉编译
$ make fetch-windivert    # 下载并校验 WinDivert 2.2.2 到 windivert/
$ VERSION=v0.1.0 make release
```

发布产物在 `dist/fastgithub-<os>-<arch>.zip`，包含可执行文件、`fastgithub.toml`、`conf.d/`、README 与 LICENSE；存在 `windivert/` 时仅随 `windows/amd64` 包一并打包。Windows 上没有 make 时可直接运行 `bash scripts/ci.sh`。

## Windows 透明拦截与服务

```console
$ fastgithub windivert-selftest   # 打印地址位域与 DNS 自检
$ fastgithub install              # 安装系统服务（需管理员/root）
$ fastgithub start                # 启动服务
$ fastgithub stop                 # 停止服务
$ fastgithub uninstall            # 卸载服务
```

WinDivert 不随包内嵌，需把 `WinDivert.dll` 与 `WinDivert64.sys` 放到 `intercept.driver_dir`、可执行文件同目录或 `windivert/` 下。DLL 缺失、非管理员或 `WinDivertOpen` 失败时只告警，38457 显式代理继续可用。WinDivert 2.2.2 仅提供 x86/x64 驱动，`windows/arm64` 包不含 `windivert/`；再分发时请保留其 `LICENSE`（LGPLv3/GPLv2）。UI 的“切换拦截”按钮当前返回 501，请通过 `intercept.mode` 配置启停。

## 目标平台

Windows / Linux / macOS，`amd64` 与 `arm64`，`CGO_ENABLED=0` 交叉编译。

## 许可证

MIT，保留原项目署名，见 [LICENSE](LICENSE)。
