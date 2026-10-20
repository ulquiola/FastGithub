package intercept

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"

	"fastgithub-go/internal/config"
)

// SelfTestText 打印地址位域与 DNS 应答解析结果，供 windivert-selftest 人工核对。
func SelfTestText() string {
	var addr Address
	addr.SetLayer(0)
	addr.SetOutbound(true)
	addr.SetNetworkIfIdx(1)

	var b strings.Builder
	fmt.Fprintf(&b, "WinDivert 可用: %v\n", Available())
	fmt.Fprintf(&b, "地址: Layer=%d Outbound=%v Loopback=%v Impostor=%v IfIdx=%d\n",
		addr.Layer(), addr.Outbound(), addr.Loopback(), addr.Impostor(), addr.NetworkIfIdx())
	fmt.Fprintf(&b, "原始地址: % x\n", addr[:])

	cfg := config.Default()
	cfg.Domains = map[string]*config.Domain{"github.com": {}}
	store, err := config.NewStore(cfg)
	if err != nil {
		fmt.Fprintf(&b, "DNS 自检: Store 创建失败: %v\n", err)
		return b.String()
	}
	query := sampleQuery("github.com", 1, 0x1234)
	resp, ok, err := DNSResponse(query, store)
	if err != nil || !ok {
		fmt.Fprintf(&b, "DNS 自检: 未命中或解析失败 (ok=%v err=%v)\n", ok, err)
		return b.String()
	}
	// 问题段 12 + (1+6) + (1+3) + 1 + 4 = 28，答案 rdata 在 40。
	if len(resp) < 44 {
		fmt.Fprintf(&b, "DNS 自检: 应答长度异常 %d\n", len(resp))
		return b.String()
	}
	fmt.Fprintf(&b, "DNS 自检: github.com A -> %s\n", net.IP(resp[40:44]))
	return b.String()
}

func sampleQuery(name string, qtype uint16, id uint16) []byte {
	msg := make([]byte, 12)
	binary.BigEndian.PutUint16(msg[0:2], id)
	binary.BigEndian.PutUint16(msg[2:4], 0x0100)
	binary.BigEndian.PutUint16(msg[4:6], 1)
	for _, label := range strings.Split(name, ".") {
		msg = append(msg, byte(len(label)))
		msg = append(msg, label...)
	}
	msg = append(msg, 0)
	msg = binary.BigEndian.AppendUint16(msg, qtype)
	msg = binary.BigEndian.AppendUint16(msg, 1)
	return msg
}
