package dialer

import (
	"net"
	"sync"
	"time"
)

type idleConn struct {
	conn    net.Conn
	expires time.Time
}

// Pool 是按 key 复用的空闲 TCP 连接池。
type Pool struct {
	mu          sync.Mutex
	max         int
	idleTimeout time.Duration
	entries     map[string][]idleConn
	now         func() time.Time
}

// NewPool 创建连接池；max 是每个 key 的最大空闲连接数。
func NewPool(max int, idleTimeout time.Duration) *Pool {
	return &Pool{
		max:         max,
		idleTimeout: idleTimeout,
		entries:     map[string][]idleConn{},
		now:         time.Now,
	}
}

// Get 取出一个未过期的空闲连接。
func (p *Pool) Get(key string) (net.Conn, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	list := p.entries[key]
	now := p.now()
	for i := len(list) - 1; i >= 0; i-- {
		if now.Before(list[i].expires) {
			conn := list[i].conn
			p.entries[key] = append(list[:i], list[i+1:]...)
			return conn, true
		}
		list[i].conn.Close()
	}
	delete(p.entries, key)
	return nil, false
}

// Put 放回一个空闲连接；超过上限时关闭最旧的一条。
func (p *Pool) Put(key string, conn net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	list := p.entries[key]
	now := p.now()
	kept := list[:0]
	for _, item := range list {
		if now.Before(item.expires) {
			kept = append(kept, item)
		} else {
			item.conn.Close()
		}
	}
	if len(kept) >= p.max {
		kept[0].conn.Close()
		kept = kept[1:]
	}
	p.entries[key] = append(kept, idleConn{conn: conn, expires: now.Add(p.idleTimeout)})
}

// CloseIdle 关闭并清空所有空闲连接。
func (p *Pool) CloseIdle() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, list := range p.entries {
		for _, item := range list {
			item.conn.Close()
		}
	}
	p.entries = map[string][]idleConn{}
}
