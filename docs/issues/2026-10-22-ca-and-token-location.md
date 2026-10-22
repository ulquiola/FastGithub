# Issue：CA 与 ui.token 存放在部署目录，重建 dist 就换一套

> 状态：待处理 ｜ 优先级：P2（不影响功能，影响可运维性与用户信任链）｜ 记录于 2026-10-22

## 1. 现象

今天在 Windows 上查信任库，发现同一个 `CN=FastGithub` 有 **4 个不同指纹**：

```
904920AAB81896B050D4EFBBC48CB9347D4AF06D  2026-10-22 09:04  ← 当前实例
446720C8C7B4D97CD38A8E034DEF0087CEB2ECA0  2026-10-21 15:26
66C037B44C9ACDEEC618E160845426401F3BEE30  2026-10-21 09:13
7F9B1B4342EBA41F94D55DFF94E8C904C14B2A40  2026-10-21 09:44
```

`LocalMachine\Root` 与 `CurrentUser\Root` 各挂一遍。这些时间点都对应"重建 dist 后第一次启动"。

## 2. 根因

- `scripts/release.sh` 每次发布先 `rm -rf dist`；
- CA 的默认目录是**部署目录下的 `cacert/`**（`internal/certs/ca.go:42` `CACertDir`）；
- 首次运行时若 `cacert/` 不存在就生成一套新 CA（`internal/certs/ca.go` `LoadOrCreate`）；
- 用户在 UI 点"安装 CA"（或命令行 `install-cert`）后，新 CA 进信任库，旧的不动。

`ui.token` 同理：它由 `internal/app/run.go` 写到 `filepath.Join(filepath.Dir(caDir), "ui.token")`，重建 dist 后令牌也跟着换，浏览器里已打开的旧页面立即 401。

## 3. 影响

1. 信任库累积多个同名 CA，用户无法判断哪个是当前的；卸载时也只删当前那个。
2. 每次升级都要重装一次证书，否则浏览器对 MITM 站点报证书错误。
3. 已经打开的 Web UI 在升级后令牌失效，表现为"突然不能操作"。
4. 多份 dist（例如同时保留 v0.4.1 与 v0.5.0）会各自生成 CA，互相不认。

## 4. 建议方案

把**证书与令牌从部署目录迁到用户数据目录**，程序目录只放二进制与配置：

| 平台 | 建议目录 |
|---|---|
| Windows | `%LOCALAPPDATA%\FastGithub\` |
| macOS | `~/Library/Application Support/FastGithub/` |
| Linux | `$XDG_STATE_HOME/fastgithub`（缺省 `~/.local/state/fastgithub`） |

解析优先级（高 → 低）：

1. 命令行 `--dir`（保持现有 `install-cert --dir` 语义，用于测试与特殊部署）
2. 环境变量 `FASTGITHUB_DATA_DIR`
3. 上述平台默认目录
4. 兜底：部署目录（兼容 portable 用法）

**迁移**：启动时若数据目录没有 `cacert/`、而部署目录有，则复制过去（不是移动，避免只读介质失败），并在日志里记一条；`ui.token` 同理。

## 5. 验收标准

- 连续两次 `release.sh` + 两次启动，CA 指纹保持不变（数据目录复用）
- 升级后已在浏览器打开的 UI 仍可操作（令牌复用）
- 全新机器首次运行会在数据目录生成 CA，且 `install-cert` 安装的是它
- 指定 `--dir` 时行为与今天完全一致（测试与脚本不受影响）

## 6. 风险与注意

- **卸载语义**：`uninstall-cert` 只应从信任库移除证书，不要顺手删数据目录里的 CA 私钥（否则下次启动又生成新的）。
- **权限**：数据目录属当前用户，`%LOCALAPPDATA%` 无需管理员；但 `install-cert` 仍需管理员写 `LocalMachine\Root`。
- **向后兼容**：迁移必须在任何 `LoadOrCreate` 之前完成，否则会先生成新 CA 再发现旧的存在。
- **文档与脚本**：`README`、`install-cert --dir` 说明、`fastgithub.toml` 注释都要同步；`scripts/release.sh` 不再需要把 `cacert/` 排除（本来也没打进包）。

## 7. 不采纳的方案

- **把 `cacert/` 打进发布包**：私钥随包分发，任何人拿到同一个 CA，等于全网共用一张中间人证书，绝不接受。
- **只改 release.sh 不删 dist**：治标，用户手动换目录或解压新包时问题照旧。