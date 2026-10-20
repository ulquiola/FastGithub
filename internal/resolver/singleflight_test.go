package resolver

import (
	"context"
	"errors"
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

	fn := func(context.Context) ([]net.IP, error) {
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
			ips, err := g.Do(context.Background(), "k", fn)
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
	fn := func(context.Context) ([]net.IP, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(20 * time.Millisecond)
		return nil, boom
	}
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := g.Do(context.Background(), "k", fn); err != boom {
				t.Errorf("err = %v，期望共享同一错误", err)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("fn 调用 %d 次，期望 1", got)
	}
}

func TestFlightCallerCancellation(t *testing.T) {
	g := newFlightGroup()
	started := make(chan struct{})
	release := make(chan struct{})
	fn := func(context.Context) ([]net.IP, error) {
		close(started)
		<-release
		return []net.IP{net.ParseIP("1.1.1.1")}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	go func() {
		<-started
		time.Sleep(50 * time.Millisecond)
		close(release)
	}()
	if _, err := g.Do(ctx, "k", fn); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v，期望调用方超时", err)
	}
}
