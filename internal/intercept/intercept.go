package intercept

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"fastgithub-go/internal/config"
)

// ErrUnsupported 表示当前平台不支持透明拦截。
var ErrUnsupported = errors.New("当前平台不支持透明拦截")

// Config 是透明拦截的运行时参数。
type Config struct {
	Store       *config.Store
	PortMap     map[int]int // 实际监听端口 -> 首选端口（443/80/22/9418）
	DriverDir   string
	DisableAAAA bool // IPv6 监听不可用时用 NODATA 促使客户端回退 IPv4
}

// RemapTCPPorts 按端口映射改写源/目的端口，返回新端口与是否修改。
func RemapTCPPorts(src, dst uint16, actualToPreferred, preferredToActual map[int]int) (uint16, uint16, bool) {
	if actual, ok := preferredToActual[int(dst)]; ok {
		return src, uint16(actual), true
	}
	if preferred, ok := actualToPreferred[int(src)]; ok {
		return uint16(preferred), dst, true
	}
	return src, dst, false
}

// TCPFilter 为每个 实际->首选 端口对生成 loopback TCP 过滤器。
func TCPFilter(portMap map[int]int) string {
	actuals := make([]int, 0, len(portMap))
	for actual := range portMap {
		actuals = append(actuals, actual)
	}
	sort.Ints(actuals)
	parts := make([]string, 0, len(actuals)*2)
	for _, actual := range actuals {
		parts = append(parts, fmt.Sprintf("tcp.DstPort == %d", portMap[actual]))
		parts = append(parts, fmt.Sprintf("tcp.SrcPort == %d", actual))
	}
	if len(parts) == 0 {
		return "loopback and false"
	}
	return "loopback and (" + strings.Join(parts, " or ") + ")"
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
