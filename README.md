# FastGithub Go

FastGithub 的 Go 重写版：解决 `github.com` 打不开、头像加载失败、release 下载与 `git clone/pull/push` 失败的问题。

> 上游 `github.com/dotnetcore/FastGithub` 已消失，本项目基于其 `v2.1.5` 备份源码重写，保持行为对等、配置语义兼容。

- 不做翻墙，不加密流量，不加速游戏
- 只支持仍被发行方支持的操作系统
- 单一静态二进制，无运行时依赖，不再需要外部 dnscrypt-proxy 进程

## 状态

配置内核（M1-A）与证书模块（M1-B）已完成。完整设计与可行性分析见 [docs/specs/2026-10-20-fastgithub-go-design.md](docs/specs/2026-10-20-fastgithub-go-design.md)。

```console
$ fastgithub config validate
配置有效: 15 条域名规则，代理端口 38457，UI 端口 38470
$ fastgithub config validate --path ./fastgithub.toml
$ fastgithub install-cert
CA 证书: cacert/fastgithub.crt
已加入系统信任库
$ fastgithub version
```

`install-cert` 会在 `cacert/` 生成 CA（已存在则复用），并把证书加入系统信任库；Windows 需要管理员权限，Linux/macOS 需要 root/sudo。Windows 下路径分隔符显示为 `\`。

实现按里程碑推进：

| 里程碑 | 内容 | 状态 |
|---|---|---|
| M1 | 配置 / DNS / 拨号 / 证书 / MITM / 流量统计 + 38457 代理 + PAC + Web UI 骨架 | 进行中（M1-A 配置内核、M1-B 证书已完成） |
| M2 | Windows WinDivert 透明拦截 + 服务安装 | 未开始 |
| M3 | 配置片段转写、兼容端点、行为对齐核对、CI 与发布脚本 | 未开始 |

## 目标平台

Windows / Linux / macOS，`amd64` 与 `arm64`，`CGO_ENABLED=0` 交叉编译。

## 许可证

MIT，保留原项目署名，见 [LICENSE](LICENSE)。