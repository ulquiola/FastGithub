# macOS 菜单栏应用设计（单进程，Go + cgo/AppKit）

> 状态：设计已确认，待实现
> 日期：2026-10-22
> 目标：把 macOS 上的 FastGithub 从“终端里跑 `fastgithub run`”变成可双击的 `.app`，常驻菜单栏，不显示控制台和 Dock 图标，常驻内存目标 <30MB。

## 1. 背景与目标

当前 macOS 使用方式：

```console
$ ./fastgithub run
代理已启动: 127.0.0.1:38457
UI: http://127.0.0.1:38470/
```

问题：

- 必须从终端启动，关掉终端或按 Ctrl+C 就停止；
- 没有菜单栏入口，系统代理、证书、退出都要打开 Web UI；
- 直接双击 Go 二进制会弹终端窗口（或没有任何界面）。

目标：

1. 双击 `.app` 启动，无控制台窗口、无 Dock 图标，只在菜单栏显示图标。
2. 菜单栏提供：运行状态、打开 Web UI、系统代理开关、证书安装/卸载、开机启动、退出。
3. 单进程：菜单栏 UI 与代理在同一个 Go 进程内，常驻内存目标 <30MB。
4. 数据持久化到 `~/Library/Application Support/FastGithub`，升级/重建 `.app` 不换 CA 和 `ui.token`。
5. 本机自用：ad-hoc 签名，不做公证、不做 App Store 分发。

非目标：

- 不实现 Dock 图标、窗口式设置界面（设置继续用现有 Web UI）。
- 不实现 universal binary（本机 arm64；构建脚本按当前架构编译）。
- 不自动启用系统代理、不自动安装 CA（避免未经确认修改系统状态）。
- 不实现 macOS 透明拦截（平台不支持，菜单显示“不支持”）。
- 不改变现有 CLI `fastgithub run` 的行为。

## 2. 进程模型

单进程：

```text
FastGithub.app
└─ FastGithub (Go + cgo)
   ├─ 主线程：runtime.LockOSThread + NSApplication 事件循环 + NSStatusItem 菜单
   ├─ goroutine：app.Run(ctx, cfg, store, ready)  → 代理 + Web UI
   └─ goroutine：启动日志、LaunchAgent 维护
```

- `main` 先 `runtime.LockOSThread()`，再初始化数据目录、加载配置、获取单实例锁、启动代理，最后调用 ObjC 的 `fg_run_menu()` 进入 AppKit 主循环。
- AppKit 回调（菜单点击）在 Go 主线程上执行，调用 Go 导出的动作函数；动作完成后更新菜单。
- 退出：菜单“退出”调用 Go 的 cancel，等待 `app.Run` 返回（关闭监听、关闭系统代理），再 `[NSApp terminate]`。
- 已有实例时（单实例锁被占用）：读取配置里的 `ui_port`，用 `NSWorkspace` 打开 `http://127.0.0.1:<ui_port>/`，然后退出本次启动。
- 单实例锁的范围是数据目录。菜单栏应用与从仓库目录运行的 CLI 使用不同锁文件；两者同时启动时不会互相拦截，但会争抢 38457/38470 端口，后启动的一方会收到明确的“监听端口失败”错误。

## 3. App 包与数据目录

### 3.1 包结构

```text
FastGithub.app/
  Contents/
    Info.plist
    MacOS/FastGithub          # cgo 菜单栏 + 代理二进制
    Resources/
      fastgithub.toml         # 默认配置
      conf.d/                 # 默认域名规则
      AppIcon.icns            # 可选
```

`Info.plist` 关键键：

| 键 | 值 |
|---|---|
| `CFBundleIdentifier` | `com.fastgithub.menubar` |
| `CFBundleExecutable` | `FastGithub` |
| `CFBundleName` / `CFBundleDisplayName` | `FastGithub` |
| `CFBundlePackageType` | `APPL` |
| `LSUIElement` | `true`（不显示 Dock 图标） |
| `LSMinimumSystemVersion` | `13.0` |
| `NSHighResolutionCapable` | `true` |
| `CFBundleIconFile` | `AppIcon`（可选） |

### 3.2 数据目录

`~/Library/Application Support/FastGithub/`：

```text
fastgithub.toml
conf.d/
cacert/fastgithub.crt
cacert/fastgithub.key
ui.token
logs/fastgithub.log
logs/app.log
```

首次启动：

1. 创建数据目录；
2. 若 `fastgithub.toml` 不存在，从 `Contents/Resources/fastgithub.toml` 复制；
3. 遍历 `Contents/Resources/conf.d`，只复制数据目录里缺失的文件，不覆盖已有规则；
4. 升级时同样只补充缺失的默认文件，用户改过的配置和规则保持不变。

启动后 `os.Chdir(数据目录)`，现有 `certs.DefaultDir`（`cacert`）和 `ui.token` 的相对路径都会落在数据目录。

### 3.3 与旧 CLI 数据的关系

不自动迁移仓库里的 `cacert/`。新应用首次使用菜单“安装 CA”安装新 CA；旧 CA 若仍在钥匙串里，用户可手动删除。README/发布说明里写清楚这一点。

## 4. 菜单行为

```text
FastGithub
  代理：运行中 · 127.0.0.1:38457        （只读）
  透明拦截：macOS 不支持                （只读）
  ─────────
  打开 Web 界面
  系统代理：已启用（点击关闭） / 未启用（点击启用）
  ─────────
  安装 CA / 卸载 CA                    （按状态二选一）
  开机启动 ✓
  ─────────
  退出 FastGithub
```

状态刷新：

- 菜单打开时（`NSMenuDelegate menuWillOpen`）重新读取：
  - 代理是否运行、实际端口；
  - 系统代理状态（`sysproxy.Get`）；
  - 证书状态（`certs.Status`）；
  - 开机启动是否启用（LaunchAgent 文件是否存在）。
- 动作执行后立即刷新菜单。

各动作：

| 动作 | 行为 | 失败处理 |
|---|---|---|
| 打开 Web 界面 | `NSWorkspace` 打开 `http://127.0.0.1:<ui_port>/` | 弹 `NSAlert` |
| 系统代理开关 | `sysproxy.Enable/Disable(proxy_port)` | 弹 `NSAlert`，菜单显示实际状态 |
| 安装/卸载 CA | `certs.LoadOrCreate(数据目录/cacert)` 确保 CA 存在，再 `certs.Install/Uninstall(certPath)` | 弹 `NSAlert`，保留原状态 |
| 开机启动 | 写入/删除 `~/Library/LaunchAgents/com.fastgithub.menubar.plist` | 弹 `NSAlert` |
| 退出 | cancel → 等代理收尾 → `[NSApp terminate]` | 超时 5s 后强制退出 |

系统代理在 VPN 默认路由下会返回 `supported:false` + `note`；菜单显示“不可用（VPN）”，点击时弹提示，不修改系统设置。

## 5. 开机启动

使用用户级 LaunchAgent，不依赖 `SMAppService`（ad-hoc 签名 + 本机自用更可控）：

`~/Library/LaunchAgents/com.fastgithub.menubar.plist`：

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.fastgithub.menubar</string>
  <key>ProgramArguments</key>
  <array><string>/Users/<user>/Applications/FastGithub.app/Contents/MacOS/FastGithub</string></array>
  <key>RunAtLoad</key><true/>
  <key>LimitLoadToSessionType</key><string>Aqua</string>
</dict>
</plist>
```

- 勾选：写 plist → `launchctl bootstrap gui/<uid> <plist>`；
- 取消：`launchctl bootout gui/<uid>/com.fastgithub.menubar` → 删除 plist；
- 每次启动时若已启用开机启动，用当前可执行文件路径重写 plist，避免应用被移动后失效；
- 不加 `KeepAlive`，否则用户从菜单退出后会被 launchd 拉起。

## 6. 构建、签名与安装

新增 `scripts/build-macos-app.sh` 与 `make macapp`：

```bash
CGO_ENABLED=1 GOOS=darwin GOARCH=$(go env GOARCH) \
  go build -trimpath -ldflags "$LDFLAGS" \
  -o dist/FastGithub.app/Contents/MacOS/FastGithub \
  ./cmd/fastgithub-menubar
```

步骤：

1. 清理 `dist/FastGithub.app`；
2. 编译菜单栏二进制（cgo）；
3. 复制 `fastgithub.toml`、`conf.d/`、可选图标到 `Contents/Resources`；
4. 生成 `Info.plist`；
5. `codesign --force --deep -s - dist/FastGithub.app`（ad-hoc）；
6. 可选 `cp -R` 到 `~/Applications/FastGithub.app`（本机自用，无需管理员）。

CLI `cmd/fastgithub` 保持 `CGO_ENABLED=0` 和六目标交叉编译不变；菜单栏命令只在 macOS + cgo 下编译。

## 7. 代码结构

| 位置 | 内容 |
|---|---|
| `cmd/fastgithub-menubar/main_darwin.go` | 数据目录、配置加载、单实例、启动代理、进入 AppKit 主循环 |
| `cmd/fastgithub-menubar/menubar_darwin.m` | `NSStatusItem`、菜单、`NSAlert`、`NSWorkspace` |
| `cmd/fastgithub-menubar/actions_darwin.go` | 菜单动作：系统代理/证书/自启/退出；导出给 ObjC |
| `cmd/fastgithub-menubar/main_other.go` | 非 macOS 占位（`//go:build !darwin`），保证 `go build ./...` |
| `cmd/fastgithub-menubar/main_nocgo_darwin.go` | macOS 无 cgo 占位（`//go:build darwin && !cgo`） |
| `internal/macapp` | 数据目录初始化、默认文件复制、LaunchAgent 读写、状态文案（可单测） |
| `scripts/build-macos-app.sh` | 构建、签名、安装 |
| `Makefile` | `macapp` 目标 |

菜单栏图标：使用系统 SF Symbol（如 `network`）的 template image，不新增图片资源；Finder 图标可选，用现有 `fastgithub.ico` 转 `AppIcon.icns`。

## 8. 错误处理与日志

- 启动失败（端口占用、配置错误、单实例冲突）：
  - 单实例冲突：打开 UI 后退出；
  - 其他错误：写 `logs/app.log`，弹 `NSAlert`，退出。
- 菜单动作失败：`NSAlert` 显示中文错误，菜单状态保持不变。
- `log` 标准输出重定向到 `logs/app.log`；请求日志仍由 `flow.Logger` 写 `logs/fastgithub.log`。
- 退出时若系统代理由本程序启用，现有 `app.Run` 的 defer 会关闭它；菜单退出等待收尾完成。

## 9. 测试与验收

### 9.1 自动化

- `internal/macapp`：
  - 数据目录初始化；
  - 默认配置只复制一次，不覆盖用户文件；
  - LaunchAgent plist 生成、路径更新、删除；
  - 菜单状态文案（运行中/系统代理/VPN 不可用/证书状态）。
- 构建：
  - macOS：`CGO_ENABLED=1 go build ./cmd/fastgithub-menubar`；
  - 非 macOS：`GOOS=windows/linux go build ./...` 不受影响；
  - `go test ./...`、`go vet ./...`、`gofmt -l .` 干净。

### 9.2 手工验收

1. 双击 `~/Applications/FastGithub.app`：无控制台、无 Dock 图标，菜单栏出现图标。
2. 菜单显示“代理：运行中 · 127.0.0.1:38457”；打开 Web UI 正常。
3. 系统代理开关：启用后 `scutil --proxy` 指向 38457；关闭后恢复。
4. 证书：安装后 `security verify-cert` 通过，菜单变为“卸载 CA”；卸载后恢复。
5. 开机启动：勾选后重启（或注销）自动出现菜单栏图标；取消后不再自启。
6. 退出：菜单退出后进程消失，系统代理被关闭，端口释放。
7. 二次启动：已有实例时只打开 Web UI，不产生第二个进程。
8. 资源：空闲 5 分钟后 `footprint -p <pid>` 的 `phys_footprint` <30MB（即 Activity Monitor 的“内存”）。`ps -o rss` 会包含 AppKit 共享库，不作为验收指标。

## 10. 风险与缓解

| 风险 | 缓解 |
|---|---|
| cgo/Objective-C 与 Go 主线程交互复杂 | 先做最小 spike：一个 NSStatusItem + 一个菜单项 + Go 回调；验证通过再铺开 |
| 菜单动作阻塞主线程（`security` 可能等待授权） | 第一版同步执行；若明显卡顿，改为 goroutine + `dispatch_async` 回主线程更新 |
| cgo 无法交叉编译 | macOS 包只在 macOS 构建；CLI 仍保持六目标交叉编译 |
| ad-hoc 签名被 Gatekeeper 拦截 | 本机构建无 quarantine；文档说明首次打开方式 |
| 应用移动导致 LaunchAgent 路径失效 | 每次启动重写 plist 为当前路径 |
| 新数据目录导致旧 CA 不匹配 | 首次安装新 CA；文档说明旧证书清理方式 |

## 11. 里程碑

1. **M1（spike）**：最小 cgo 菜单栏，能显示图标、一个“退出”菜单项，代理在 goroutine 运行。
2. **M2**：数据目录、默认配置复制、单实例、启动代理、打开 Web UI。
3. **M3**：系统代理开关、证书安装/卸载、状态刷新。
4. **M4**：开机启动、构建脚本、`.app` 打包与 ad-hoc 签名、安装到 `~/Applications`。
5. **M5**：测试、内存测量、文档与发布说明。

## 12. 验收标准

- 双击 `.app` 即可使用，无终端窗口、无 Dock 图标；
- 菜单栏功能完整（状态、打开界面、系统代理、证书、开机启动、退出）；
- 常驻物理内存（`footprint`）<30MB；
- 升级/重建 `.app` 后 CA 与 `ui.token` 不变；
- 现有 CLI 与 Windows/Linux 构建不受影响；
- `go test ./...`、`go vet ./...`、`gofmt -l .` 全绿。
