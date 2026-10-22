# macOS 菜单栏应用实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 macOS 上的 FastGithub 做成双击即用的 `.app`：菜单栏常驻、无控制台/Dock 图标、单进程、内存 <30MB，数据持久化到 `~/Library/Application Support/FastGithub`。

**Architecture:** 一个 Go 二进制通过 cgo 直接调用 AppKit；主线程跑 `NSApplication` 事件循环，代理在 goroutine 里跑现有 `app.Run`。菜单动作通过 `//export` 的 Go 函数完成，状态用 JSON 从 Go 传给 Objective-C。`.app` 用 ad-hoc 签名，LaunchAgent 实现开机启动。

**Tech Stack:** Go 1.22+（本机 1.27.1）、cgo、Objective-C/AppKit、macOS 13+、零第三方 Go 依赖。

**Spec:** `docs/specs/2026-10-22-macos-menubar-design.md`

## Global Constraints

- 仅 macOS 13+、本机 arm64；菜单栏二进制用 `CGO_ENABLED=1` 构建。
- 现有 CLI `cmd/fastgithub` 保持 `CGO_ENABLED=0` 和六目标交叉编译不变。
- 零第三方 Go 依赖；只使用标准库 + 系统框架（Cocoa/Foundation）。
- 数据目录固定为 `~/Library/Application Support/FastGithub`。
- 不做公证、不做 App Store、不自动启用系统代理、不自动安装 CA。
- 每个任务结束前：`gofmt -l .` 无输出、相关测试通过。
- 菜单栏应用内存目标 <30MB（`ps -o rss` 验收）。

---

### Task 1: cgo 菜单栏 spike

**Files:**
- Create: `cmd/fastgithub-menubar/main_darwin.go`
- Create: `cmd/fastgithub-menubar/menubar.h`
- Create: `cmd/fastgithub-menubar/menubar.m`
- Create: `cmd/fastgithub-menubar/main_other.go`
- Create: `cmd/fastgithub-menubar/main_nocgo_darwin.go`

**Interfaces:**
- Produces: `void fg_run_menu(void)`（Objective-C，进入 AppKit 主循环）
- Produces: 可执行文件 `fastgithub-menubar`，双击/运行后出现菜单栏图标

- [ ] **Step 1: 写最小 Go 入口**

`cmd/fastgithub-menubar/main_darwin.go`：

```go
//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include "menubar.h"
*/
import "C"

import "runtime"

func main() {
	// NSApplication 必须跑在主线程上。
	runtime.LockOSThread()
	C.fg_run_menu()
}
```

- [ ] **Step 2: 写 Objective-C 头文件**

`cmd/fastgithub-menubar/menubar.h`：

```c
#ifndef FASTGITHUB_MENUBAR_H
#define FASTGITHUB_MENUBAR_H

void fg_run_menu(void);

#endif
```

- [ ] **Step 3: 写最小菜单栏**

`cmd/fastgithub-menubar/menubar.m`：

```objc
#import <Cocoa/Cocoa.h>
#import "menubar.h"

@interface FGController : NSObject <NSApplicationDelegate>
@property (strong) NSStatusItem *statusItem;
@end

@implementation FGController

- (void)applicationDidFinishLaunching:(NSNotification *)note {
    self.statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
    NSImage *image = [NSImage imageWithSystemSymbolName:@"network" accessibilityDescription:@"FastGithub"];
    image.template = YES;
    self.statusItem.button.image = image;

    NSMenu *menu = [[NSMenu alloc] init];
    NSMenuItem *quit = [[NSMenuItem alloc] initWithTitle:@"退出 FastGithub" action:@selector(terminate:) keyEquivalent:@"q"];
    quit.target = NSApp;
    [menu addItem:quit];
    self.statusItem.menu = menu;
}

@end

void fg_run_menu(void) {
    @autoreleasepool {
        NSApplication *app = [NSApplication sharedApplication];
        [app setActivationPolicy:NSApplicationActivationPolicyAccessory];
        FGController *controller = [[FGController alloc] init];
        app.delegate = controller;
        [app run];
    }
}
```

- [ ] **Step 4: 写非 macOS / 无 cgo 占位**

`cmd/fastgithub-menubar/main_other.go`：

```go
//go:build !darwin

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "FastGithub 菜单栏应用仅支持 macOS")
	os.Exit(1)
}
```

`cmd/fastgithub-menubar/main_nocgo_darwin.go`：

```go
//go:build darwin && !cgo

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "macOS 菜单栏应用需要 CGO_ENABLED=1 构建")
	os.Exit(1)
}
```

- [ ] **Step 5: 构建**

Run:

```bash
CGO_ENABLED=1 go build -o /tmp/fg-menubar ./cmd/fastgithub-menubar
```

Expected: 构建成功，无输出。

- [ ] **Step 6: 手工验证**

Run:

```bash
/tmp/fg-menubar
```

Expected:

- 菜单栏出现一个网络图标；
- 没有 Dock 图标、没有终端窗口（从终端启动时终端仍占用，但应用本身不弹窗）；
- 点击图标出现“退出 FastGithub”，点击后进程退出。

- [ ] **Step 7: 提交**

```bash
git add cmd/fastgithub-menubar
git commit -m "feat(macapp): cgo 菜单栏 spike"
```

---

### Task 2: 数据目录、默认配置与 LaunchAgent

**Files:**
- Create: `internal/macapp/appdata.go`
- Create: `internal/macapp/appdata_test.go`
- Create: `internal/macapp/launchagent.go`
- Create: `internal/macapp/launchagent_test.go`

**Interfaces:**
- Produces: `func DataDir(home string) string`
- Produces: `func EnsureDefaults(dataDir, resourcesDir string) error`
- Produces: `func LaunchAgentPath(home string) string`
- Produces: `func WriteLaunchAgent(home, executable string) error`
- Produces: `func RemoveLaunchAgent(home string) error`
- Produces: `func LaunchAgentEnabled(home string) bool`

- [ ] **Step 1: 写失败测试**

`internal/macapp/appdata_test.go`：

```go
package macapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureDefaultsCopiesMissingOnly(t *testing.T) {
	home := t.TempDir()
	resources := filepath.Join(home, "Resources")
	dataDir := DataDir(home)

	if err := os.MkdirAll(filepath.Join(resources, "conf.d"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "fastgithub.toml"), []byte("default"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "conf.d", "10-github.toml"), []byte("github"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resources, "conf.d", "20-google.toml"), []byte("google"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := EnsureDefaults(dataDir, resources); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dataDir, "fastgithub.toml")); string(got) != "default" {
		t.Fatalf("config = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dataDir, "conf.d", "10-github.toml")); string(got) != "github" {
		t.Fatalf("conf = %q", got)
	}

	// 用户改过的文件不能被覆盖。
	if err := os.WriteFile(filepath.Join(dataDir, "fastgithub.toml"), []byte("user"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDefaults(dataDir, resources); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dataDir, "fastgithub.toml")); string(got) != "user" {
		t.Fatalf("用户配置被覆盖: %q", got)
	}

	// 新增的默认规则应补进来。
	if err := os.WriteFile(filepath.Join(resources, "conf.d", "30-microsoft.toml"), []byte("ms"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureDefaults(dataDir, resources); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dataDir, "conf.d", "30-microsoft.toml")); string(got) != "ms" {
		t.Fatalf("缺失规则未补充: %q", got)
	}
}
```

`internal/macapp/launchagent_test.go`：

```go
package macapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchAgentLifecycle(t *testing.T) {
	home := t.TempDir()
	exe := filepath.Join(home, "Applications", "FastGithub.app", "Contents", "MacOS", "FastGithub")
	if LaunchAgentEnabled(home) {
		t.Fatal("初始不应启用")
	}
	if err := WriteLaunchAgent(home, exe); err != nil {
		t.Fatal(err)
	}
	if !LaunchAgentEnabled(home) {
		t.Fatal("写入后应启用")
	}
	data, err := os.ReadFile(LaunchAgentPath(home))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		"com.fastgithub.menubar",
		"<key>RunAtLoad</key><true/>",
		"<key>LimitLoadToSessionType</key><string>Aqua</string>",
		exe,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("plist 缺少 %q:\n%s", want, text)
		}
	}
	if err := RemoveLaunchAgent(home); err != nil {
		t.Fatal(err)
	}
	if LaunchAgentEnabled(home) {
		t.Fatal("删除后应禁用")
	}
	if err := RemoveLaunchAgent(home); err != nil {
		t.Fatalf("重复删除应幂等: %v", err)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run:

```bash
go test ./internal/macapp -count=1
```

Expected: 编译失败，`DataDir` / `EnsureDefaults` / `LaunchAgentPath` 未定义。

- [ ] **Step 3: 实现数据目录**

`internal/macapp/appdata.go`：

```go
// Package macapp 提供 macOS 应用的数据目录、默认配置和 LaunchAgent 管理。
package macapp

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DataDir 返回应用数据目录。
func DataDir(home string) string {
	return filepath.Join(home, "Library", "Application Support", "FastGithub")
}

// EnsureDefaults 在数据目录缺失时复制默认配置与规则，不覆盖已有文件。
func EnsureDefaults(dataDir, resourcesDir string) error {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return fmt.Errorf("创建数据目录 %s 失败: %w", dataDir, err)
	}
	if err := copyIfMissing(filepath.Join(resourcesDir, "fastgithub.toml"), filepath.Join(dataDir, "fastgithub.toml")); err != nil {
		return err
	}
	confSrc := filepath.Join(resourcesDir, "conf.d")
	entries, err := os.ReadDir(confSrc)
	if err != nil {
		return fmt.Errorf("读取默认规则目录 %s 失败: %w", confSrc, err)
	}
	confDst := filepath.Join(dataDir, "conf.d")
	if err := os.MkdirAll(confDst, 0700); err != nil {
		return fmt.Errorf("创建规则目录 %s 失败: %w", confDst, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := copyIfMissing(filepath.Join(confSrc, entry.Name()), filepath.Join(confDst, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func copyIfMissing(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开默认文件 %s 失败: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0644)
	if err != nil {
		return fmt.Errorf("创建 %s 失败: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return fmt.Errorf("复制 %s 失败: %w", src, err)
	}
	return out.Close()
}
```

- [ ] **Step 4: 实现 LaunchAgent**

`internal/macapp/launchagent.go`：

```go
package macapp

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const launchAgentLabel = "com.fastgithub.menubar"

// LaunchAgentPath 返回用户级 LaunchAgent 的 plist 路径。
func LaunchAgentPath(home string) string {
	return filepath.Join(home, "Library", "LaunchAgents", launchAgentLabel+".plist")
}

// LaunchAgentEnabled 判断开机启动 plist 是否存在。
func LaunchAgentEnabled(home string) bool {
	_, err := os.Stat(LaunchAgentPath(home))
	return err == nil
}

// WriteLaunchAgent 写入开机启动 plist。
func WriteLaunchAgent(home, executable string) error {
	path := LaunchAgentPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("创建 LaunchAgents 目录失败: %w", err)
	}
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	b.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	b.WriteString("<plist version=\"1.0\">\n<dict>\n")
	fmt.Fprintf(&b, "  <key>Label</key><string>%s</string>\n", xmlEscape(launchAgentLabel))
	b.WriteString("  <key>ProgramArguments</key>\n  <array>\n")
	fmt.Fprintf(&b, "    <string>%s</string>\n", xmlEscape(executable))
	b.WriteString("  </array>\n")
	b.WriteString("  <key>RunAtLoad</key><true/>\n")
	b.WriteString("  <key>LimitLoadToSessionType</key><string>Aqua</string>\n")
	b.WriteString("</dict>\n</plist>\n")
	return os.WriteFile(path, []byte(b.String()), 0644)
}

// RemoveLaunchAgent 删除开机启动 plist；不存在时视为成功。
func RemoveLaunchAgent(home string) error {
	err := os.Remove(LaunchAgentPath(home))
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 LaunchAgent 失败: %w", err)
	}
	return nil
}

func xmlEscape(s string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(s))
	return b.String()
}
```

- [ ] **Step 5: 运行测试**

Run:

```bash
go test ./internal/macapp -count=1 -v
```

Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add internal/macapp
git commit -m "feat(macapp): 数据目录、默认配置与 LaunchAgent"
```

---

### Task 3: 菜单状态文案

**Files:**
- Create: `internal/macapp/state.go`
- Create: `internal/macapp/state_test.go`

**Interfaces:**
- Produces: `type MenuState struct { Running bool; ProxyAddr string; SystemProxy string; SystemProxyNote string; CertInstalled bool; Autostart bool; Error string }`
- Produces: `func (s MenuState) ProxyTitle() string`
- Produces: `func (s MenuState) SystemProxyTitle() string`
- Produces: `func (s MenuState) CertTitle() string`

- [ ] **Step 1: 写失败测试**

`internal/macapp/state_test.go`：

```go
package macapp

import "testing"

func TestProxyTitle(t *testing.T) {
	if got := (MenuState{}).ProxyTitle(); got != "代理：未运行" {
		t.Fatalf("got %q", got)
	}
	if got := (MenuState{Running: true, ProxyAddr: "127.0.0.1:38457"}).ProxyTitle(); got != "代理：运行中 · 127.0.0.1:38457" {
		t.Fatalf("got %q", got)
	}
}

func TestSystemProxyTitle(t *testing.T) {
	cases := []struct {
		state MenuState
		want  string
	}{
		{MenuState{SystemProxy: "enabled"}, "系统代理：已启用（点击关闭）"},
		{MenuState{SystemProxy: "disabled"}, "系统代理：未启用（点击启用）"},
		{MenuState{SystemProxy: "unsupported", SystemProxyNote: "默认路由为 VPN"}, "系统代理：不可用（VPN）"},
		{MenuState{SystemProxy: "unsupported"}, "系统代理：不可用"},
	}
	for _, c := range cases {
		if got := c.state.SystemProxyTitle(); got != c.want {
			t.Fatalf("got %q, want %q", got, c.want)
		}
	}
}

func TestCertTitle(t *testing.T) {
	if got := (MenuState{}).CertTitle(); got != "安装 CA" {
		t.Fatalf("got %q", got)
	}
	if got := (MenuState{CertInstalled: true}).CertTitle(); got != "卸载 CA" {
		t.Fatalf("got %q", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run:

```bash
go test ./internal/macapp -run TestProxyTitle -count=1
```

Expected: 编译失败，`MenuState` 未定义。

- [ ] **Step 3: 实现**

`internal/macapp/state.go`：

```go
package macapp

// MenuState 是菜单栏需要展示的状态。
type MenuState struct {
	Running         bool   `json:"running"`
	ProxyAddr       string `json:"proxyAddr"`
	SystemProxy     string `json:"systemProxy"` // enabled | disabled | unsupported
	SystemProxyNote string `json:"systemProxyNote"`
	CertInstalled   bool   `json:"certInstalled"`
	Autostart       bool   `json:"autostart"`
	Error           string `json:"error"`
}

// ProxyTitle 返回代理状态行。
func (s MenuState) ProxyTitle() string {
	if !s.Running {
		if s.Error != "" {
			return "代理：未运行（" + s.Error + "）"
		}
		return "代理：未运行"
	}
	return "代理：运行中 · " + s.ProxyAddr
}

// SystemProxyTitle 返回系统代理菜单项文案。
func (s MenuState) SystemProxyTitle() string {
	switch s.SystemProxy {
	case "enabled":
		return "系统代理：已启用（点击关闭）"
	case "disabled":
		return "系统代理：未启用（点击启用）"
	default:
		if s.SystemProxyNote != "" {
			return "系统代理：不可用（VPN）"
		}
		return "系统代理：不可用"
	}
}

// CertTitle 返回证书菜单项文案。
func (s MenuState) CertTitle() string {
	if s.CertInstalled {
		return "卸载 CA"
	}
	return "安装 CA"
}
```

- [ ] **Step 4: 运行测试**

Run:

```bash
go test ./internal/macapp -count=1 -v
```

Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/macapp/state.go internal/macapp/state_test.go
git commit -m "feat(macapp): 菜单状态文案"
```

---

### Task 4: 启动代理、状态 JSON 与打开 Web UI

**Files:**
- Modify: `cmd/fastgithub-menubar/main_darwin.go`
- Create: `cmd/fastgithub-menubar/actions_darwin.go`
- Modify: `cmd/fastgithub-menubar/menubar.h`
- Modify: `cmd/fastgithub-menubar/menubar.m`

**Interfaces:**
- Consumes: `macapp.DataDir`、`EnsureDefaults`、`MenuState`
- Produces: `char *fg_state_json(void)`（Go 导出）
- Produces: `int fg_open_ui(void)`（Go 导出）

- [ ] **Step 1: 扩展 Go 入口**

`cmd/fastgithub-menubar/main_darwin.go` 替换为：

```go
//go:build darwin && cgo

package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa
#include "menubar.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"fastgithub-go/internal/app"
	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/instance"
	"fastgithub-go/internal/macapp"
)

var (
	mu        sync.Mutex
	cfg       *config.Config
	dataDir   string
	ca        *certs.Authority
	proxyAddr string
	uiAddr    string
	runErr    string
	lastErr   string
)

func main() {
	runtime.LockOSThread()
	if err := run(); err != nil {
		log.Printf("启动失败: %v", err)
		os.Exit(1)
	}
}

func run() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dataDir = macapp.DataDir(home)
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	resources := filepath.Join(filepath.Dir(filepath.Dir(exe)), "Resources")
	if err := macapp.EnsureDefaults(dataDir, resources); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "logs"), 0700); err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(dataDir, "logs", "app.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer logFile.Close()
	log.SetOutput(logFile)

	if err := os.Chdir(dataDir); err != nil {
		return err
	}
	loaded, err := config.Load(filepath.Join(dataDir, "fastgithub.toml"), filepath.Join(dataDir, "conf.d"))
	if err != nil {
		return err
	}
	store, err := config.NewStore(loaded)
	if err != nil {
		return err
	}
	authority, err := certs.LoadOrCreate(filepath.Join(dataDir, "cacert"))
	if err != nil {
		return err
	}
	mu.Lock()
	cfg, ca = loaded, authority
	mu.Unlock()

	lock, err := instance.Acquire(dataDir)
	if err != nil {
		if errors.Is(err, instance.ErrAlreadyRunning) {
			return openURL(fmt.Sprintf("http://127.0.0.1:%d/", loaded.Server.UIPort))
		}
		return err
	}
	defer lock.Release()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := app.Run(ctx, loaded, store, func(proxy, ui net.Addr) {
			mu.Lock()
			proxyAddr, uiAddr = proxy.String(), ui.String()
			mu.Unlock()
		})
		if err != nil {
			mu.Lock()
			runErr = err.Error()
			mu.Unlock()
			log.Printf("代理退出: %v", err)
		}
	}()

	// 等代理就绪最多 5 秒；失败也进菜单，便于查看错误并退出。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		ready := uiAddr != ""
		mu.Unlock()
		if ready {
			break
		}
		select {
		case <-done:
			goto menu
		case <-time.After(50 * time.Millisecond):
		}
	}
menu:
	C.fg_run_menu()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	return nil
}

func openURL(url string) error {
	return execCommand("open", url)
}
```

- [ ] **Step 2: 实现 Go 导出函数**

`cmd/fastgithub-menubar/actions_darwin.go`：

```go
//go:build darwin && cgo

package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"log"
	"os/exec"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/macapp"
	"fastgithub-go/internal/sysproxy"
)

func setError(err error) C.int {
	mu.Lock()
	if err != nil {
		lastErr = err.Error()
	} else {
		lastErr = ""
	}
	mu.Unlock()
	if err != nil {
		log.Printf("菜单操作失败: %v", err)
		return -1
	}
	return 0
}

//export fg_state_json
func fg_state_json() *C.char {
	mu.Lock()
	state := macapp.MenuState{
		Running:   uiAddr != "",
		ProxyAddr: proxyAddr,
		Error:     runErr,
	}
	port := 0
	if cfg != nil {
		port = cfg.Server.ProxyPort
	}
	authority := ca
	mu.Unlock()

	if port != 0 {
		if status, err := sysproxy.Get(port); err == nil {
			switch {
			case !status.Supported:
				state.SystemProxy = "unsupported"
				state.SystemProxyNote = status.Note
			case status.Enabled:
				state.SystemProxy = "enabled"
			default:
				state.SystemProxy = "disabled"
			}
		}
	}
	if authority != nil {
		if status, err := certs.Status(authority.CertPath()); err == nil {
			state.CertInstalled = status.Installed
		}
	}
	if home, err := osUserHomeDir(); err == nil {
		state.Autostart = macapp.LaunchAgentEnabled(home)
	}
	data, _ := json.Marshal(state)
	return C.CString(string(data))
}

//export fg_last_error
func fg_last_error() *C.char {
	mu.Lock()
	defer mu.Unlock()
	return C.CString(lastErr)
}

//export fg_open_ui
func fg_open_ui() C.int {
	mu.Lock()
	addr := uiAddr
	mu.Unlock()
	if addr == "" {
		return setError(errNoUI)
	}
	if err := execCommand("open", "http://"+addr+"/"); err != nil {
		return setError(err)
	}
	return 0
}

func execCommand(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}
```

在同文件补两个小辅助（避免在主文件重复导入）：

```go
var errNoUI = errors.New("代理尚未就绪")

func osUserHomeDir() (string, error) { return os.UserHomeDir() }
```

并在 import 中加 `errors`、`os`。

- [ ] **Step 3: 更新 Objective-C 菜单**

`menubar.m` 改为读取状态 JSON：

```objc
#import <Cocoa/Cocoa.h>
#import <stdlib.h>
#import "menubar.h"
#import "_cgo_export.h"

@interface FGController : NSObject <NSApplicationDelegate, NSMenuDelegate>
@property (strong) NSStatusItem *statusItem;
@property (strong) NSMenuItem *proxyItem;
@property (strong) NSMenuItem *interceptItem;
@property (strong) NSMenuItem *openItem;
@end

@implementation FGController

- (void)applicationDidFinishLaunching:(NSNotification *)note {
    self.statusItem = [[NSStatusBar systemStatusBar] statusItemWithLength:NSVariableStatusItemLength];
    NSImage *image = [NSImage imageWithSystemSymbolName:@"network" accessibilityDescription:@"FastGithub"];
    image.template = YES;
    self.statusItem.button.image = image;

    NSMenu *menu = [[NSMenu alloc] init];
    menu.delegate = self;

    self.proxyItem = [[NSMenuItem alloc] initWithTitle:@"代理：启动中…" action:nil keyEquivalent:@""];
    self.proxyItem.enabled = NO;
    [menu addItem:self.proxyItem];

    self.interceptItem = [[NSMenuItem alloc] initWithTitle:@"透明拦截：macOS 不支持" action:nil keyEquivalent:@""];
    self.interceptItem.enabled = NO;
    [menu addItem:self.interceptItem];
    [menu addItem:[NSMenuItem separatorItem]];

    self.openItem = [[NSMenuItem alloc] initWithTitle:@"打开 Web 界面" action:@selector(openUI:) keyEquivalent:@"o"];
    self.openItem.target = self;
    [menu addItem:self.openItem];
    [menu addItem:[NSMenuItem separatorItem]];

    NSMenuItem *quit = [[NSMenuItem alloc] initWithTitle:@"退出 FastGithub" action:@selector(terminate:) keyEquivalent:@"q"];
    quit.target = NSApp;
    [menu addItem:quit];

    self.statusItem.menu = menu;
    [self refresh];
}

- (void)menuWillOpen:(NSMenu *)menu { [self refresh]; }

- (void)refresh {
    char *raw = fg_state_json();
    NSString *json = raw ? @(raw) : @"{}";
    free(raw);
    NSData *data = [json dataUsingEncoding:NSUTF8StringEncoding];
    NSDictionary *state = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil] ?: @{};
    self.proxyItem.title = [state[@"running"] boolValue]
        ? [NSString stringWithFormat:@"代理：运行中 · %@", state[@"proxyAddr"]]
        : @"代理：未运行";
}

- (void)openUI:(id)sender {
    if (fg_open_ui() != 0) {
        [self showError];
    }
}

- (void)showError {
    char *raw = fg_last_error();
    NSString *text = raw ? @(raw) : @"未知错误";
    free(raw);
    NSAlert *alert = [[NSAlert alloc] init];
    alert.messageText = @"FastGithub";
    alert.informativeText = text;
    [alert runModal];
}

@end

void fg_run_menu(void) {
    @autoreleasepool {
        NSApplication *app = [NSApplication sharedApplication];
        [app setActivationPolicy:NSApplicationActivationPolicyAccessory];
        FGController *controller = [[FGController alloc] init];
        app.delegate = controller;
        [app run];
    }
}
```

`menubar.h` 保持 `void fg_run_menu(void);` 不变。

- [ ] **Step 4: 构建**

Run:

```bash
CGO_ENABLED=1 go build -o /tmp/fg-menubar ./cmd/fastgithub-menubar
```

Expected: 构建成功。

- [ ] **Step 5: 手工验证**

先准备一个临时数据目录（或直接运行，首次会创建 `~/Library/Application Support/FastGithub`）：

```bash
/tmp/fg-menubar
```

Expected:

- 菜单栏出现图标；
- 菜单显示“代理：运行中 · 127.0.0.1:38457”；
- 点击“打开 Web 界面”能打开 `http://127.0.0.1:38470/`；
- 退出后进程结束，端口释放。

- [ ] **Step 6: 提交**

```bash
git add cmd/fastgithub-menubar
git commit -m "feat(macapp): 启动代理、状态与打开 Web UI"
```

---

### Task 5: 系统代理、证书与开机启动菜单项

**Files:**
- Modify: `cmd/fastgithub-menubar/actions_darwin.go`
- Modify: `cmd/fastgithub-menubar/menubar.m`
- Modify: `internal/macapp/launchagent.go`
- Modify: `internal/macapp/launchagent_test.go`

**Interfaces:**
- Consumes: `sysproxy.Get/Enable/Disable`、`certs.Status/Install/Uninstall`、`macapp.LaunchAgentEnabled/Write/Remove`
- Produces: `int fg_toggle_proxy(void)`、`int fg_toggle_cert(void)`、`int fg_toggle_autostart(void)`

- [ ] **Step 1: 写 LaunchAgent 命令辅助的失败测试**

在 `internal/macapp/launchagent_test.go` 追加：

```go
func TestLaunchAgentCommandArgs(t *testing.T) {
	home := t.TempDir()
	exe := "/Applications/FastGithub.app/Contents/MacOS/FastGithub"
	if err := WriteLaunchAgent(home, exe); err != nil {
		t.Fatal(err)
	}
	if got := BootstrapArgs(home, 501); len(got) != 3 ||
		got[0] != "bootstrap" || got[1] != "gui/501" || got[2] != LaunchAgentPath(home) {
		t.Fatalf("bootstrap args = %v", got)
	}
	if got := BootoutArgs(501); len(got) != 2 || got[0] != "bootout" || got[1] != "gui/501/com.fastgithub.menubar" {
		t.Fatalf("bootout args = %v", got)
	}
}
```

- [ ] **Step 2: 运行确认失败**

Run:

```bash
go test ./internal/macapp -run TestLaunchAgentCommandArgs -count=1
```

Expected: 编译失败，`BootstrapArgs` / `BootoutArgs` 未定义。

- [ ] **Step 3: 实现命令参数**

在 `internal/macapp/launchagent.go` 追加：

```go
// BootstrapArgs 返回 launchctl bootstrap 的参数。
func BootstrapArgs(home string, uid int) []string {
	return []string{"bootstrap", fmt.Sprintf("gui/%d", uid), LaunchAgentPath(home)}
}

// BootoutArgs 返回 launchctl bootout 的参数。
func BootoutArgs(uid int) []string {
	return []string{"bootout", fmt.Sprintf("gui/%d/%s", uid, launchAgentLabel)}
}
```

- [ ] **Step 4: 实现菜单动作**

在 `cmd/fastgithub-menubar/actions_darwin.go` 追加：

```go
//export fg_toggle_proxy
func fg_toggle_proxy() C.int {
	mu.Lock()
	port := 0
	if cfg != nil {
		port = cfg.Server.ProxyPort
	}
	mu.Unlock()
	if port == 0 {
		return setError(errNoConfig)
	}
	status, err := sysproxy.Get(port)
	if err != nil {
		return setError(err)
	}
	if status.Supported && status.Enabled {
		return setError(sysproxy.Disable(port))
	}
	return setError(sysproxy.Enable(port))
}

//export fg_toggle_cert
func fg_toggle_cert() C.int {
	mu.Lock()
	authority := ca
	mu.Unlock()
	if authority == nil {
		return setError(errNoConfig)
	}
	status, err := certs.Status(authority.CertPath())
	if err != nil {
		return setError(err)
	}
	if status.Installed {
		return setError(certs.Uninstall(authority.CertPath()))
	}
	return setError(certs.Install(authority.CertPath()))
}

//export fg_toggle_autostart
func fg_toggle_autostart() C.int {
	home, err := os.UserHomeDir()
	if err != nil {
		return setError(err)
	}
	if macapp.LaunchAgentEnabled(home) {
		_ = execCommand("launchctl", macapp.BootoutArgs(os.Getuid())...)
		return setError(macapp.RemoveLaunchAgent(home))
	}
	exe, err := os.Executable()
	if err != nil {
		return setError(err)
	}
	if err := macapp.WriteLaunchAgent(home, exe); err != nil {
		return setError(err)
	}
	if err := execCommand("launchctl", macapp.BootstrapArgs(home, os.Getuid())...); err != nil {
		return setError(err)
	}
	return 0
}

var (
	errNoConfig = errors.New("配置未加载")
)
```

- [ ] **Step 5: 扩展菜单**

在 `menubar.m` 的属性区加：

```objc
@property (strong) NSMenuItem *systemProxyItem;
@property (strong) NSMenuItem *certItem;
@property (strong) NSMenuItem *autostartItem;
```

在 `applicationDidFinishLaunching` 的“打开 Web 界面”之后加：

```objc
self.systemProxyItem = [[NSMenuItem alloc] initWithTitle:@"系统代理：检测中…" action:@selector(toggleProxy:) keyEquivalent:@""];
self.systemProxyItem.target = self;
[menu addItem:self.systemProxyItem];
[menu addItem:[NSMenuItem separatorItem]];

self.certItem = [[NSMenuItem alloc] initWithTitle:@"安装 CA" action:@selector(toggleCert:) keyEquivalent:@""];
self.certItem.target = self;
[menu addItem:self.certItem];

self.autostartItem = [[NSMenuItem alloc] initWithTitle:@"开机启动" action:@selector(toggleAutostart:) keyEquivalent:@""];
self.autostartItem.target = self;
[menu addItem:self.autostartItem];
[menu addItem:[NSMenuItem separatorItem]];
```

在 `refresh` 里补：

```objc
NSString *sp = state[@"systemProxy"] ?: @"";
if ([sp isEqualToString:@"enabled"]) {
    self.systemProxyItem.title = @"系统代理：已启用（点击关闭）";
    self.systemProxyItem.enabled = YES;
} else if ([sp isEqualToString:@"disabled"]) {
    self.systemProxyItem.title = @"系统代理：未启用（点击启用）";
    self.systemProxyItem.enabled = YES;
} else {
    self.systemProxyItem.title = state[@"systemProxyNote"] ? @"系统代理：不可用（VPN）" : @"系统代理：不可用";
    self.systemProxyItem.enabled = NO;
}
self.certItem.title = [state[@"certInstalled"] boolValue] ? @"卸载 CA" : @"安装 CA";
self.autostartItem.state = [state[@"autostart"] boolValue] ? NSControlStateValueOn : NSControlStateValueOff;
```

加动作方法：

```objc
- (void)toggleProxy:(id)sender {
    if (fg_toggle_proxy() != 0) { [self showError]; }
    [self refresh];
}

- (void)toggleCert:(id)sender {
    if (fg_toggle_cert() != 0) { [self showError]; }
    [self refresh];
}

- (void)toggleAutostart:(id)sender {
    if (fg_toggle_autostart() != 0) { [self showError]; }
    [self refresh];
}
```

- [ ] **Step 6: 构建并手工验证**

Run:

```bash
CGO_ENABLED=1 go build -o /tmp/fg-menubar ./cmd/fastgithub-menubar
/tmp/fg-menubar
```

Expected:

- 系统代理项可在启用/关闭之间切换，`scutil --proxy` 同步变化；
- 证书项显示“安装 CA”，点击后变为“卸载 CA”，`security verify-cert` 通过；
- 开机启动勾选后生成 `~/Library/LaunchAgents/com.fastgithub.menubar.plist`，取消后删除；
- 任一动作失败会弹 `NSAlert`。

- [ ] **Step 7: 提交**

```bash
git add cmd/fastgithub-menubar internal/macapp
git commit -m "feat(macapp): 系统代理、证书与开机启动菜单"
```

---

### Task 6: .app 打包、签名、安装与文档

**Files:**
- Create: `scripts/build-macos-app.sh`
- Modify: `Makefile`
- Modify: `README.md`
- Modify: `docs/handoff-2026-10-21.md`

**Interfaces:**
- Produces: `dist/FastGithub.app`，可选安装到 `~/Applications/FastGithub.app`

- [ ] **Step 1: 写构建脚本**

`scripts/build-macos-app.sh`：

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "$(uname -s)" != "Darwin" ]; then
  echo "macOS 应用只能在 macOS 上构建" >&2
  exit 1
fi

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
COMMIT="$(git rev-parse --short HEAD 2>/dev/null || echo none)"
BUILD_TIME="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
LDFLAGS="-s -w -X fastgithub-go/internal/app.version=$VERSION -X fastgithub-go/internal/app.commit=$COMMIT -X fastgithub-go/internal/app.buildTime=$BUILD_TIME"

APP="dist/FastGithub.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"

CGO_ENABLED=1 GOOS=darwin GOARCH="$(go env GOARCH)" \
  go build -trimpath -ldflags "$LDFLAGS" \
  -o "$APP/Contents/MacOS/FastGithub" ./cmd/fastgithub-menubar

cp fastgithub.toml "$APP/Contents/Resources/"
cp -R conf.d "$APP/Contents/Resources/"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleIdentifier</key><string>com.fastgithub.menubar</string>
  <key>CFBundleName</key><string>FastGithub</string>
  <key>CFBundleDisplayName</key><string>FastGithub</string>
  <key>CFBundleExecutable</key><string>FastGithub</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>${VERSION}</string>
  <key>CFBundleVersion</key><string>${COMMIT}</string>
  <key>LSUIElement</key><true/>
  <key>LSMinimumSystemVersion</key><string>13.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

codesign --force --deep -s - "$APP"
echo "已生成 $APP"

if [ "${INSTALL:-0}" = "1" ]; then
  mkdir -p "$HOME/Applications"
  rm -rf "$HOME/Applications/FastGithub.app"
  cp -R "$APP" "$HOME/Applications/"
  echo "已安装到 $HOME/Applications/FastGithub.app"
fi
```

- [ ] **Step 2: 加 Makefile 目标**

```make
macapp:
	./scripts/build-macos-app.sh

macapp-install:
	INSTALL=1 ./scripts/build-macos-app.sh
```

并把它们加入 `.PHONY`。

- [ ] **Step 3: 构建并安装**

Run:

```bash
chmod +x scripts/build-macos-app.sh
make macapp-install
```

Expected:

- `dist/FastGithub.app` 生成；
- `~/Applications/FastGithub.app` 存在；
- `codesign -dv ~/Applications/FastGithub.app` 显示 ad-hoc 签名。

- [ ] **Step 4: 手工验收**

Run:

```bash
open ~/Applications/FastGithub.app
```

Expected:

- 无终端窗口、无 Dock 图标；
- 菜单栏出现图标；
- 菜单项完整：代理状态、透明拦截不支持、打开 Web 界面、系统代理、证书、开机启动、退出；
- 空闲 5 分钟后 `ps -o rss= -p $(pgrep -f 'FastGithub.app/Contents/MacOS/FastGithub')` 输出 <30000（KB）。

- [ ] **Step 5: 更新文档**

README 增加“macOS 应用”一节：

```markdown
## macOS 菜单栏应用

```console
$ make macapp-install
$ open ~/Applications/FastGithub.app
```

- 无控制台、无 Dock 图标，菜单栏常驻；
- 数据在 `~/Library/Application Support/FastGithub`，升级不换 CA/token；
- 菜单可切换系统代理、安装/卸载 CA、设置开机启动；
- 首次使用请在菜单里安装 CA；旧的 CLI 证书可在钥匙串里手动删除。
```

交接文档追加一条：

```markdown
### v0.6 macOS 菜单栏应用（2026-10-22）

- 新增 `cmd/fastgithub-menubar`（cgo/AppKit）与 `internal/macapp`；
- 单进程菜单栏 + 代理，数据目录 `~/Library/Application Support/FastGithub`；
- `make macapp-install` 构建 ad-hoc 签名的 `.app` 并安装到 `~/Applications`；
- 菜单：状态、打开 Web UI、系统代理、证书、开机启动、退出；
- CLI 与 Windows/Linux 构建不受影响。
```

- [ ] **Step 6: 全量验证**

Run:

```bash
go test ./... -count=1
go vet ./...
gofmt -l .
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/fastgithub
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build ./cmd/fastgithub
```

Expected: 全绿、无输出、CLI 交叉编译成功。

- [ ] **Step 7: 提交**

```bash
git add scripts/build-macos-app.sh Makefile README.md docs/handoff-2026-10-21.md
git commit -m "feat(macapp): .app 打包、签名与文档"
```

---

## 自检

- **Spec 覆盖**：
  - 单进程 cgo/AppKit → Task 1/4/5；
  - 数据目录与默认配置 → Task 2；
  - 菜单状态 → Task 3/5；
  - 开机启动 → Task 2/5；
  - `.app` 打包、签名、安装 → Task 6；
  - 测试与内存验收 → Task 2/3/6。
- **类型一致性**：`macapp.MenuState`、`fg_state_json`、`fg_toggle_proxy/cert/autostart`、`BootstrapArgs/BootoutArgs` 在各任务中签名一致。
- **无占位符**：所有实现步骤给出完整代码；UI 步骤给出可执行的构建/手工验收命令。
