package resolver

import (
	"context"
	"net"
	"sync"
)

type flightCall struct {
	done chan struct{}
	ips  []net.IP
	err  error
}

// flightGroup 合并同一域名的并发查询；共享工作与首个调用方的取消解耦，
// 但每个调用方都能按自己的 ctx 提前返回。
type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flightCall
}

func newFlightGroup() *flightGroup {
	return &flightGroup{calls: map[string]*flightCall{}}
}

func (g *flightGroup) Do(ctx context.Context, key string, fn func(context.Context) ([]net.IP, error)) ([]net.IP, error) {
	g.mu.Lock()
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		select {
		case <-c.done:
			return c.ips, c.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	c := &flightCall{done: make(chan struct{})}
	g.calls[key] = c
	g.mu.Unlock()

	go func() {
		defer func() {
			close(c.done)
			g.mu.Lock()
			delete(g.calls, key)
			g.mu.Unlock()
		}()
		c.ips, c.err = fn(context.WithoutCancel(ctx))
	}()

	select {
	case <-c.done:
		return c.ips, c.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
