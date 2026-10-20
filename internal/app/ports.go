package app

import (
	"fmt"
	"net"
	"strconv"
)

// ListenTCPFallback 从 preferred 起尝试监听，端口被占用时顺延。
// preferred <= 0 时由系统分配；attempts <= 0 时默认 100 次。
func ListenTCPFallback(host string, preferred, attempts int) (net.Listener, int, error) {
	if preferred <= 0 {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			return nil, 0, err
		}
		return ln, ln.Addr().(*net.TCPAddr).Port, nil
	}
	if attempts <= 0 {
		attempts = 100
	}
	var lastErr error
	for port := preferred; port < preferred+attempts && port <= 65535; port++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
		if err == nil {
			return ln, port, nil
		}
		lastErr = err
	}
	return nil, 0, fmt.Errorf("从端口 %d 起 %d 次尝试均失败: %w", preferred, attempts, lastErr)
}
