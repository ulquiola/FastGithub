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
	"os/exec"
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
	running   bool
	runErr    string
	lastErr   string
)

func main() {
	// NSApplication 必须跑在主线程上。
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
	if _, err := os.Stat(filepath.Join(resources, "fastgithub.toml")); err != nil {
		// 开发时从仓库根目录直接运行，没有 .app/Contents/Resources。
		resources = "."
	}
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

	// 应用被移动后，开机启动仍指向旧路径；每次启动时按当前路径重写。
	if macapp.LaunchAgentEnabled(home) {
		if err := macapp.WriteLaunchAgent(home, exe); err != nil {
			log.Printf("更新开机启动失败: %v", err)
		}
	}

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
			running = true
			mu.Unlock()
		})
		mu.Lock()
		running = false
		if err != nil {
			runErr = err.Error()
		}
		mu.Unlock()
		if err != nil {
			log.Printf("代理退出: %v", err)
		}
	}()

	// 等代理就绪最多 5 秒；失败也进菜单，便于查看错误并退出。
	deadline := time.Now().Add(5 * time.Second)
waitLoop:
	for {
		mu.Lock()
		ready := running
		mu.Unlock()
		if ready || time.Now().After(deadline) {
			break
		}
		select {
		case <-done:
			break waitLoop
		case <-time.After(50 * time.Millisecond):
		}
	}

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

func execCommand(name string, args ...string) error {
	return exec.Command(name, args...).Run()
}
