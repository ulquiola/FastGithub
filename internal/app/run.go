package app

import (
	"context"
	"fmt"
	"net"
	"time"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/dialer"
	"fastgithub-go/internal/mitm"
	"fastgithub-go/internal/resolver"
)

// Run 在配置的代理端口上提供服务，直到 ctx 取消。
func Run(ctx context.Context, cfg *config.Config, store *config.Store) error {
	addr := fmt.Sprintf("127.0.0.1:%d", cfg.Server.ProxyPort)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", addr, err)
	}
	return Serve(ctx, cfg, store, ln, certs.DefaultDir)
}

// Serve 在给定监听器上服务；caDir 用于测试注入临时 CA 目录。
func Serve(ctx context.Context, cfg *config.Config, store *config.Store, ln net.Listener, caDir string) error {
	ca, err := certs.LoadOrCreate(caDir)
	if err != nil {
		ln.Close()
		return err
	}
	res := resolver.New(cfg.DNS)
	d := dialer.New(store, res, nil)
	reverse := mitm.NewReverseProxy(store, dialer.NewTransport(store, d))
	server := mitm.NewServer(store, d, ca, reverse, 5*time.Minute)

	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("接受连接失败: %w", err)
		}
		go server.ServeConn(ctx, conn)
	}
}
