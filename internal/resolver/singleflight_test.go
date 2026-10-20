package resolver

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFlightDedup(t *testing.T) {
	g := newFlightGroup()
	var calls int32
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once

	fn := func() ([]net.IP, error) {
		atomic.AddInt32(&calls, 1)
		once.Do(func() { close(started) })
		<-release
		return []net.IP{net.ParseIP("1.1.1.1")}, nil
	}

	var wg sync.WaitGroup
	results := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ips, err := g.Do("k", fn)
			if err != nil {
				results <- err
				return
			}
			if len(ips) != 1 {
				results <- errShortResult
			}
		}()
	}
	<-started
	time.Sleep(20 * time.Millisecond) // 让其余调用进入等待
	close(release)
	wg.Wait()
	close(results)
	for err := range results {
		t.Errorf("Do: %v", err)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("fn 调用 %d 次，期望 1", got)
	}
}

func TestFlightErrorShared(t *testing.T) {
	g := newFlightGroup()
	var calls int32
	boom := errShortResult
	fn := func() ([]net.IP, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(20 * time.Millisecond)
		return nil, boom
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := g.Do("k", fn); err != boom {
				t.Errorf("err = %v，期望共享同一错误", err)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("fn 调用 %d 次，期望 1", got)
	}
}
