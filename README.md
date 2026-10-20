# FastGithub Go

FastGithub 的 Go 重写版：解决 `github.com` 打不开、头像加载失败、release 下载与 `git clone/pull/push` 失败的问题。

> 上游 `github.com/dotnetcore/FastGithub` 已消失，本项目基于其 `v2.1.5` 备份源码重写，保持行为对等、配置语义兼容。

- 不做翻墙，不加密流量，不加速游戏
- 只支持仍被发行方支持的操作系统
- 单一静态二进制，无运行时依赖，不再需要外部 dnscrypt-proxy 进程

## 状态

M1 已完成：配置内核（M1-A）、证书（M1-B）、DNS 解析器（M1-C）、拨号/代理（M1-D）、流量统计与 Web UI（M1-E）。完整设计与可行性分析见 [docs/specs/2026-10-20-fastgithub-go-design.md](docs/specs/2026-10-20-fastgithub-go-design.md)。

```console
$ fastgithub config validate
配置有效: 15 条域名规则，代理端口 38457，UI 端口 38470
$ fastgithub config validate --path ./fastgithub.toml
$ fastgithub install-cert
CA 证书: cacert/fastgithub.crt
已加入系统信任库
$ fastgithub run
代理已启动: 127.0.0.1:38457
UI: http://127.0.0.1:38470/
$ fastgithub version
```

`run` 启动显式代理与 Web UI：命中域名走本地 CA 的 MITM，未命中走盲隧道，`GET http://127.0.0.1:38457/` 返回 PAC，`GET /flowStatistics` 返回 `TotalRead/TotalWrite/ReadRate/WriteRate`。UI 提供状态、域名命中、实时日志（SSE）与 CA 下载；变更接口需要启动目录下 `ui.token` 中的令牌（0600）。

`install-cert` 会在 `cacert/` 生成 CA（已存在则复用），并把证书加入系统信任库；Windows 需要管理员权限，Linux/macOS 需要 root/sudo。Windows 下路径分隔符显示为 `\`。

实现按里程碑推进：

| 里程碑 | 内容 | 状态 |
|---|---|---|
| M1 | 配置 / DNS / 拨号 / 证书 / MITM / 流量统计 + 38457 代理 + PAC + Web UI 骨架 | 已完成（M1-A~E） |
| M2 | Windows WinDivert 透明拦截 + 服务安装 | 未开始 |
| M3 | 配置片段转写、兼容端点、行为对齐核对、CI 与发布脚本 | 未开始 |

## 目标平台

Windows / Linux / macOS，`amd64` 与 `arm64`，`CGO_ENABLED=0` 交叉编译。

## 许可证

MIT，保留原项目署名，见 [LICENSE](LICENSE)。