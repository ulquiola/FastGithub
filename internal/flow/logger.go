package flow

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger 把请求日志写入环形缓冲、流量统计与可选文件。
type Logger struct {
	ring    *Ring
	tracker *Tracker

	mu   sync.Mutex
	file *os.File
}

func NewLogger(ring *Ring, tracker *Tracker, filePath string) (*Logger, error) {
	l := &Logger{ring: ring, tracker: tracker}
	if filePath == "" {
		return l, nil
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}
	file, err := os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("打开日志文件 %s 失败: %w", filePath, err)
	}
	l.file = file
	return l, nil
}

// Log 记录一条请求日志。
func (l *Logger) Log(e Entry) {
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	if l.ring != nil {
		l.ring.Add(e)
	}
	if l.tracker != nil {
		l.tracker.Add(e.Read, e.Write)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		fmt.Fprintf(l.file, "%s %s %s %d %s %d %d\n",
			e.Time.Format(time.RFC3339), e.Domain, e.Method, e.Status, e.Duration, e.Read, e.Write)
	}
}

// Write 实现 io.Writer，让标准库 log 的诊断信息与请求日志写进同一个文件。
func (l *Logger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return len(p), nil
	}
	return l.file.Write(p)
}

func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}
