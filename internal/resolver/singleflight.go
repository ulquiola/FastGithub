package resolver

import (
	"net"
	"sync"
)

type flightCall struct {
	done chan struct{}
	ips  []net.IP
	err  error
}

// flightGroup 合并同一域名的并发查询；首个调用执行 fn，其余等待结果。
type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flightCall
}

func newFlightGroup() *flightGroup {
	return &flightGroup{calls: map[string]*flightCall{}}
}

func (g *flightGroup) Do(key string, fn func() ([]net.IP, error)) ([]net.IP, error) {
	g.mu.Lock()
	if c, ok := g.calls[key]; ok {
		g.mu.Unlock()
		<-c.done
		return c.ips, c.err
	}
	c := &flightCall{done: make(chan struct{})}
	g.calls[key] = c
	g.mu.Unlock()

	c.ips, c.err = fn()
	close(c.done)

	g.mu.Lock()
	delete(g.calls, key)
	g.mu.Unlock()
	return c.ips, c.err
}
