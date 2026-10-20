package flow

import "sync"

// Ring 保存最近 max 条日志并向订阅者广播。
type Ring struct {
	mu      sync.Mutex
	max     int
	entries []Entry
	subs    map[chan Entry]struct{}
}

func NewRing(max int) *Ring {
	if max <= 0 {
		max = 1000
	}
	return &Ring{max: max, subs: map[chan Entry]struct{}{}}
}

func (r *Ring) Add(e Entry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, e)
	if len(r.entries) > r.max {
		r.entries = r.entries[len(r.entries)-r.max:]
	}
	for ch := range r.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func (r *Ring) Recent() []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Entry(nil), r.entries...)
}

// Subscribe 返回日志通道与取消函数；通道满时新日志会被丢弃。
func (r *Ring) Subscribe() (<-chan Entry, func()) {
	ch, _, cancel := r.SubscribeWithBacklog()
	return ch, cancel
}

// SubscribeWithBacklog 在同一把锁内返回 backlog 并注册订阅，避免重复或空窗。
func (r *Ring) SubscribeWithBacklog() (<-chan Entry, []Entry, func()) {
	ch := make(chan Entry, 64)
	r.mu.Lock()
	backlog := append([]Entry(nil), r.entries...)
	r.subs[ch] = struct{}{}
	r.mu.Unlock()
	return ch, backlog, func() {
		r.mu.Lock()
		delete(r.subs, ch)
		close(ch)
		r.mu.Unlock()
	}
}
