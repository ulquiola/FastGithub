package app

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/dialer"
	"fastgithub-go/internal/mitm"
	"fastgithub-go/internal/resolver"
)

// Run 在配置的代理端口上提供服务，直到 ctx 取消；ready 在监听成功后回调。
func Run(ctx context.Context, cfg *config.Config, store *config.Store, ready func(net.Addr)) error {
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Server.ProxyPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", addr, err)
	}
	return Serve(ctx, cfg, store, ln, certs.DefaultDir, ready)
}

// Serve 在给定监听器上服务；caDir 用于测试注入临时 CA 目录。
func Serve(ctx context.Context, cfg *config.Config, store *config.Store, ln net.Listener, caDir string, ready func(net.Addr)) error {
	ca, err := certs.LoadOrCreate(caDir)
	if err != nil {
		ln.Close()
		return err
	}
	if ready != nil {
		ready(ln.Addr())
	}
	res := resolver.New(cfg.DNS)
	d := dialer.New(store, res, nil)
	reverse := mitm.NewReverseProxy(store, dialer.NewTransport(store, d))
	server := mitm.NewServer(store, d, ca, reverse, 5*time.Minute)

	var mu sync.Mutex
	active := map[net.Conn]struct{}{}
	closing := false
	stop := make(chan struct{})
	shutdownDone := make(chan struct{})
	defer close(stop)
	go func() {
		select {
		case <-ctx.Done():
			ln.Close()
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
		conn, err := ln.Accept()
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
		go func(conn net.Conn) {
			defer func() {
				mu.Lock()
				delete(active, conn)
				mu.Unlock()
			}()
			server.ServeConn(ctx, conn)
		}(conn)
	}
}
