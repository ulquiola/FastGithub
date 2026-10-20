package flow

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// Entry 是一条请求日志。
type Entry struct {
	Time     time.Time
	Domain   string
	Method   string
	Status   int
	Duration time.Duration
	Read     int64
	Write    int64
}

// Sample 是一次速率采样（单位 bytes/s，假设 Tick 间隔为 1s）。
type Sample struct {
	Time      time.Time
	ReadRate  int64
	WriteRate int64
}

// Snapshot 是流量统计快照。
type Snapshot struct {
	TotalRead  int64
	TotalWrite int64
	ReadRate   int64
	WriteRate  int64
	Samples    []Sample
}

// Tracker 累计读写字节并按 Tick 采样速率。
type Tracker struct {
	read  atomic.Int64
	write atomic.Int64

	mu        sync.Mutex
	max       int
	samples   []Sample
	lastRead  int64
	lastWrite int64
	now       func() time.Time
}

func NewTracker(maxSamples int) *Tracker {
	if maxSamples <= 0 {
		maxSamples = 60
	}
	return &Tracker{max: maxSamples, now: time.Now}
}

// Add 累加字节数。
func (t *Tracker) Add(read, write int64) {
	if read != 0 {
		t.read.Add(read)
	}
	if write != 0 {
		t.write.Add(write)
	}
}

// Tick 计算自上次采样以来的增量并保留最近 max 个样本。
func (t *Tracker) Tick() {
	read := t.read.Load()
	write := t.write.Load()
	t.mu.Lock()
	defer t.mu.Unlock()
	t.samples = append(t.samples, Sample{
		Time:      t.now(),
		ReadRate:  read - t.lastRead,
		WriteRate: write - t.lastWrite,
	})
	t.lastRead, t.lastWrite = read, write
	if len(t.samples) > t.max {
		t.samples = t.samples[len(t.samples)-t.max:]
	}
}

// Snapshot 返回累计值与最近一次采样速率。
func (t *Tracker) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	s := Snapshot{
		TotalRead:  t.read.Load(),
		TotalWrite: t.write.Load(),
		Samples:    append([]Sample(nil), t.samples...),
	}
	if n := len(t.samples); n > 0 {
		s.ReadRate = t.samples[n-1].ReadRate
		s.WriteRate = t.samples[n-1].WriteRate
	}
	return s
}

// Run 按 interval 持续采样，直到 ctx 取消。
func (t *Tracker) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.Tick()
		case <-ctx.Done():
			return
		}
	}
}
