//go:build windows

package app

import (
	"context"
	"errors"
	"log"
	"net"
	"strconv"

	"fastgithub-go/internal/config"
	"fastgithub-go/internal/intercept"
	"fastgithub-go/internal/mitm"
)

// startTransparent 监听 443/80/22/9418 并启动 WinDivert；失败时返回错误由调用方降级。
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

	type entry struct {
		ln      net.Listener
		port    int
		prefer  int
		handler func(context.Context, net.Conn)
	}
	entries := []entry{
		{httpsLn, httpsPort, cfg.Server.Transparent.HTTPS, func(ctx context.Context, conn net.Conn) { server.ServeTLS(ctx, conn) }},
		{httpLn, httpPort, cfg.Server.Transparent.HTTP, func(ctx context.Context, conn net.Conn) { server.ServeHTTPConn(ctx, conn) }},
	}
	if sshLn, sshPort, sshErr := ListenTCPFallback("127.0.0.1", cfg.Server.Transparent.SSH, 100); sshErr == nil {
		entries = append(entries, entry{sshLn, sshPort, cfg.Server.Transparent.SSH, func(ctx context.Context, conn net.Conn) {
			server.ServeForward(ctx, conn, "github.com", 22, "SSH")
		}})
	} else {
		log.Printf("SSH 透明入口未启用: %v", sshErr)
	}
	if gitLn, gitPort, gitErr := ListenTCPFallback("127.0.0.1", cfg.Server.Transparent.Git, 100); gitErr == nil {
		entries = append(entries, entry{gitLn, gitPort, cfg.Server.Transparent.Git, func(ctx context.Context, conn net.Conn) {
			server.ServeForward(ctx, conn, "github.com", 9418, "GIT")
		}})
	} else {
		log.Printf("Git 透明入口未启用: %v", gitErr)
	}

	portMap := make(map[int]int, len(entries))
	v6Listeners := make([]net.Listener, 0, len(entries))
	allowAAAA := true
	for _, e := range entries {
		portMap[e.port] = e.prefer
		if v6 := listenV6(e.port); v6 != nil {
			v6Listeners = append(v6Listeners, v6)
			go acceptLoop(ctx, v6, e.handler)
		} else {
			allowAAAA = false
		}
	}

	handle, err := intercept.Start(ctx, intercept.Config{
		Store:       store,
		PortMap:     portMap,
		DriverDir:   cfg.Intercept.DriverDir,
		DisableAAAA: !allowAAAA,
	})
	if err != nil {
		for _, e := range entries {
			e.ln.Close()
		}
		for _, ln := range v6Listeners {
			ln.Close()
		}
		return func() {}, err
	}
	for _, e := range entries {
		go acceptLoop(ctx, e.ln, e.handler)
	}
	log.Printf("透明拦截已启用: %v", portMap)
	return func() {
		for _, e := range entries {
			e.ln.Close()
		}
		for _, ln := range v6Listeners {
			ln.Close()
		}
		handle.Close()
	}, nil
}

func acceptLoop(ctx context.Context, ln net.Listener, handle func(context.Context, net.Conn)) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			// 拦截启动失败或进程退出时会关闭监听，这属于正常收尾，不应记成错误。
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				return
			}
			log.Printf("透明监听 %s 退出: %v", ln.Addr(), err)
			return
		}
		go handle(ctx, conn)
	}
}

func listenV6(port int) net.Listener {
	ln, err := net.Listen("tcp", net.JoinHostPort("::1", strconv.Itoa(port)))
	if err != nil {
		log.Printf("IPv6 透明监听 %d 失败: %v", port, err)
		return nil
	}
	return ln
}
