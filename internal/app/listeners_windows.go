//go:build windows

package app

import (
	"context"
	"log"
	"net"
	"strconv"

	"fastgithub-go/internal/config"
	"fastgithub-go/internal/intercept"
	"fastgithub-go/internal/mitm"
)

// startTransparent 监听 443/80 并启动 WinDivert；失败时返回错误由调用方降级。
func startTransparent(ctx context.Context, server *mitm.Server, store *config.Store, cfg *config.Config) (func(), error) {
	if cfg.Intercept.Mode == "off" {
		return func() {}, nil
	}
	httpsLn, httpsPort, err := ListenTCPFallback("127.0.0.1", cfg.Server.Transparent.HTTPS, 100)
	if err != nil {
		return func() {}, err
	}
	httpLn, httpPort, err := ListenTCPFallback("127.0.0.1", cfg.Server.Transparent.HTTP, 100)
	if err != nil {
		httpsLn.Close()
		return func() {}, err
	}
	// DNS 的 AAAA 应答指向 ::1，IPv6 客户端需要对应的监听器。
	https6 := listenV6(httpsPort)
	http6 := listenV6(httpPort)
	portMap := map[int]int{
		httpsPort: cfg.Server.Transparent.HTTPS,
		httpPort:  cfg.Server.Transparent.HTTP,
	}
	handle, err := intercept.Start(ctx, intercept.Config{
		Store:     store,
		PortMap:   portMap,
		DriverDir: cfg.Intercept.DriverDir,
	})
	if err != nil {
		httpsLn.Close()
		httpLn.Close()
		if https6 != nil {
			https6.Close()
		}
		if http6 != nil {
			http6.Close()
		}
		return func() {}, err
	}
	go acceptLoop(ctx, httpsLn, func(ctx context.Context, conn net.Conn) { server.ServeTLS(ctx, conn) })
	go acceptLoop(ctx, httpLn, func(ctx context.Context, conn net.Conn) { server.ServeHTTPConn(ctx, conn) })
	if https6 != nil {
		go acceptLoop(ctx, https6, func(ctx context.Context, conn net.Conn) { server.ServeTLS(ctx, conn) })
	}
	if http6 != nil {
		go acceptLoop(ctx, http6, func(ctx context.Context, conn net.Conn) { server.ServeHTTPConn(ctx, conn) })
	}
	log.Printf("透明拦截已启用: 443->%d, 80->%d", httpsPort, httpPort)
	return func() {
		httpsLn.Close()
		httpLn.Close()
		if https6 != nil {
			https6.Close()
		}
		if http6 != nil {
			http6.Close()
		}
		handle.Close()
	}, nil
}

func listenV6(port int) net.Listener {
	ln, err := net.Listen("tcp", net.JoinHostPort("::1", strconv.Itoa(port)))
	if err != nil {
		log.Printf("IPv6 透明监听 %d 失败: %v", port, err)
		return nil
	}
	return ln
}

func acceptLoop(ctx context.Context, ln net.Listener, handle func(context.Context, net.Conn)) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handle(ctx, conn)
	}
}
