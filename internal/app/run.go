package app

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/dialer"
	"fastgithub-go/internal/flow"
	"fastgithub-go/internal/intercept"
	"fastgithub-go/internal/mitm"
	"fastgithub-go/internal/resolver"
	"fastgithub-go/internal/sysproxy"
	"fastgithub-go/internal/ui"
)

// Run 启动代理与 UI，直到 ctx 取消；ready 在监听成功后回调。
func Run(ctx context.Context, cfg *config.Config, store *config.Store, ready func(proxy, ui net.Addr)) error {
	proxyLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Server.ProxyPort))
	if err != nil {
		return fmt.Errorf("监听代理端口失败: %w", err)
	}
	uiLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Server.UIPort))
	if err != nil {
		proxyLn.Close()
		return fmt.Errorf("监听 UI 端口失败: %w", err)
	}
	return serve(ctx, cfg, store, proxyLn, uiLn, certs.DefaultDir, ready)
}

// Serve 在给定代理监听器上服务，并自行监听 server.ui_port。
func Serve(ctx context.Context, cfg *config.Config, store *config.Store, ln net.Listener, caDir string, ready func(proxy, ui net.Addr)) error {
	uiLn, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Server.UIPort))
	if err != nil {
		ln.Close()
		return fmt.Errorf("监听 UI 端口失败: %w", err)
	}
	return serve(ctx, cfg, store, ln, uiLn, caDir, ready)
}

func serve(ctx context.Context, cfg *config.Config, store *config.Store, proxyLn, uiLn net.Listener, caDir string, ready func(proxy, ui net.Addr)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	ca, err := certs.LoadOrCreate(caDir)
	if err != nil {
		proxyLn.Close()
		uiLn.Close()
		return err
	}

	tracker := flow.NewTracker(60)
	go tracker.Run(ctx, time.Second)
	ring := flow.NewRing(1000)
	logger, err := flow.NewLogger(ring, tracker, cfg.Log.File)
	if err != nil {
		proxyLn.Close()
		uiLn.Close()
		return err
	}
	defer logger.Close()
	// 托盘模式已 detachConsole，启动/降级诊断必须同时落盘，否则日志文件永远是空的。
	defer attachDiagnostics(logger)()

	res := resolver.New(cfg.DNS)
	d := dialer.New(store, res, nil)
	transport := dialer.NewTransport(store, d, dialer.WithHTTP2(cfg.Server.HTTP2))
	reverse := mitm.NewReverseProxy(store, transport)
	if cfg.Server.HTTP2 {
		// Upgrade/WebSocket 不能在 h2 上透传，固定走 h1 出口。
		reverse.SetUpgradeTransport(dialer.NewTransport(store, d))
	}
	reverse.SetLogger(logger)
	server := mitm.NewServer(store, d, ca, reverse, 5*time.Minute)
	server.SetHTTP2(cfg.Server.HTTP2)
	server.SetLogger(logger)

	token, err := ui.LoadOrCreateToken(filepath.Join(filepath.Dir(caDir), "ui.token"))
	if err != nil {
		proxyLn.Close()
		uiLn.Close()
		return err
	}
	uiServer := ui.New(ui.Options{
		Config: cfg, Store: store, Tracker: tracker, Ring: ring, CA: ca,
		Token: token, Version: Version(), InstallCert: certs.Install,
	})
	server.SetStats(ui.FlowStatistics(tracker))

	uiHTTP := &http.Server{Handler: uiServer.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go uiHTTP.Serve(uiLn)
	defer uiHTTP.Close()

	// 进程退出后系统代理必须关闭，否则整机网络会指向已停止的端口。
	defer func() {
		if status, err := sysproxy.Get(cfg.Server.ProxyPort); err == nil && status.Enabled {
			if err := sysproxy.Disable(cfg.Server.ProxyPort); err != nil {
				log.Printf("退出时关闭系统代理失败: %v", err)
			}
		}
	}()

	cleanupTransparent, err := startTransparent(ctx, server, store, cfg)
	if err != nil {
		if intercept.IsPermissionError(err) {
			log.Printf("透明拦截未启用：需要管理员权限，已降级为显式代理 http://127.0.0.1:%d", cfg.Server.ProxyPort)
		} else {
			log.Printf("透明拦截未启用: %v", err)
		}
	}
	defer cleanupTransparent()

	if ready != nil {
		ready(proxyLn.Addr(), uiLn.Addr())
	}

	var mu sync.Mutex
	active := map[net.Conn]struct{}{}
	var wg sync.WaitGroup
	closing := false
	stop := make(chan struct{})
	shutdownDone := make(chan struct{})
	defer close(stop)
	defer func() {
		cancel()
		mu.Lock()
		for conn := range active {
			conn.Close()
		}
		mu.Unlock()
		wg.Wait()
	}()
	go func() {
		select {
		case <-ctx.Done():
			proxyLn.Close()
			uiHTTP.Close()
			mu.Lock()
			closing = true
			for conn := range active {
				conn.Close()
			}
			mu.Unlock()
			close(shutdownDone)
		case <-stop:
		}
	}()
	for {
		conn, err := proxyLn.Accept()
		if err != nil {
			if ctx.Err() != nil {
				<-shutdownDone
				return nil
			}
			return fmt.Errorf("接受连接失败: %w", err)
		}
		mu.Lock()
		if closing {
			mu.Unlock()
			conn.Close()
			continue
		}
		active[conn] = struct{}{}
		mu.Unlock()
		wg.Add(1)
		go func(conn net.Conn) {
			defer func() {
				mu.Lock()
				delete(active, conn)
				mu.Unlock()
				wg.Done()
			}()
			server.ServeConn(ctx, conn)
		}(conn)
	}
}
