package intercept

import (
	"context"
	"errors"

	"fastgithub-go/internal/config"
)

// ErrUnsupported 表示当前平台不支持透明拦截。
var ErrUnsupported = errors.New("当前平台不支持透明拦截")

// Config 是透明拦截的运行时参数。
type Config struct {
	Store     *config.Store
	PortMap   map[int]int // 实际监听端口 -> 首选端口（443/80）
	DriverDir string
}

// Handle 持有拦截循环的生命周期。
type Handle struct {
	cancel  context.CancelFunc
	closeFn func()
}

func (h *Handle) Close() error {
	if h == nil {
		return nil
	}
	if h.cancel != nil {
		h.cancel()
	}
	if h.closeFn != nil {
		h.closeFn()
	}
	return nil
}
