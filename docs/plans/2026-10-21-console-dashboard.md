# 控制台状态面板（托盘降级路径）实现计划

> 背景：这台机器的会话没有 Explorer，通知区域不存在，`Shell_NotifyIconW` 必然 `Access is denied`。
> 目标：无托盘时自动进入一个常驻控制台面板，仍然"双击即用"，并保留托盘给正常桌面。

## 1. 行为

| 场景 | 行为 |
|---|---|
| `fastgithub`（无参） | 先尝试托盘；**托盘不可用** → 控制台面板；托盘可用 → 只托盘 |
| `fastgithub dashboard` | 强制控制台面板（便于测试） |
| `fastgithub tray` | 只托盘（失败则报错退出，不再降级） |
| `fastgithub run` | 保持原样（纯日志） |

面板启动时自动打开浏览器 UI；可 `--no-open` 关闭。

## 2. 面板内容

```
FastGithub v0.2.0   代理 127.0.0.1:38457   UI http://127.0.0.1:38470/
透明拦截：未启用（需要管理员）        已运行 00:03:12

[1] 打开控制台界面   [2] 复制代理地址   [3] 安装证书   [q] 退出
```

- 每 2 秒原地刷新（`\r` + 清行，非 TTY 时退化为每 30 秒打印一行）。
- 单键响应，无需回车（Windows：`SetConsoleMode` 关闭 `ENABLE_LINE_INPUT`；其它平台按行读）。

## 3. 文件

| 文件 | 职责 |
|---|---|
| `internal/dashboard/dashboard.go` | 跨平台：`Options`、状态文本、按键分发表、`Run(ctx, Options) error` |
| `internal/dashboard/input_windows.go` | 单键读取（`GetStdHandle`/`GetConsoleMode`/`SetConsoleMode`/`ReadFile`） |
| `internal/dashboard/input_other.go` | 按行读取，映射首字符 |
| `internal/dashboard/actions_windows.go` | 打开 URL、复制、提权装证书（`ShellExecuteW`） |
| `internal/dashboard/actions_other.go` | 打开 URL（`xdg-open`/`open`）、复制（尽力而为）、证书提示 |
| `cmd/fastgithub/main.go` | 无参/`dashboard` 接线、`--no-open` |
| `internal/tray/tray.go` | 导出 `CopyToClipboard`/`OpenURL`/`RunElevated` 供面板复用（内部实现已存在） |

## 4. 接口

```go
type Options struct {
    Version   string
    ProxyAddr string
    UIAddr    string
    Intercept string // "已启用" / "未启用（需要管理员）" / "已关闭"
    OpenUI    func() error
    CopyProxy func() error
    InstallCert func() error
    NoTTY     bool
}

func StatusText(o Options, uptime time.Duration) string
func HandleKey(o Options, key byte) (quit bool)
func Run(ctx context.Context, o Options) error
```

`HandleKey` 是纯函数，单测覆盖 1/2/3/q/未知键。

## 5. 测试

| 测试 | 断言 |
|---|---|
| `TestStatusText` | 含版本、两个地址、拦截状态、已运行时长 |
| `TestHandleKey` | 1/2/3 调对应回调；q 返回 quit；其它键无副作用 |
| `TestFormatUptime` | 秒/分/时格式 |
| `TestStubActions`（非 Windows） | 不 panic，返回合理错误 |

## 6. 验收

1. `fastgithub`（本机无 Explorer）→ 进入面板，浏览器自动打开 UI。
2. 按 `2` 后剪贴板是 `127.0.0.1:38457`；按 `1` 重新打开界面；`q` 退出后面板消失、端口释放。
3. `fastgithub tray` 在本机报错退出（不降级）；`fastgithub run` 行为不变。
4. 六目标交叉编译通过，`go test ./...`/`go vet`/`gofmt` 干净。