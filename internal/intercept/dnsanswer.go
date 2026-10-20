package intercept

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"

	"fastgithub-go/internal/config"
)

const dnsTTL = 300

// DNSResponse 为命中域名的 A/AAAA 查询构造应答；未命中返回 ok=false。
func DNSResponse(query []byte, store *config.Store) ([]byte, bool, error) {
	if len(query) < 12 {
		return nil, false, fmt.Errorf("DNS 查询过短")
	}
	if query[2]&0x80 != 0 {
		return nil, false, nil // 已经是响应
	}
	if binary.BigEndian.Uint16(query[4:6]) != 1 {
		return nil, false, nil
	}
	name, off, err := parseQName(query, 12)
	if err != nil {
		return nil, false, err
	}
	if off+4 > len(query) {
		return nil, false, fmt.Errorf("DNS 问题段截断")
	}
	qtype := binary.BigEndian.Uint16(query[off:])
	if qtype != 1 && qtype != 28 {
		return nil, false, nil
	}
	if _, matched := store.Lookup(name); !matched {
		return nil, false, nil
	}

	resp := make([]byte, off+4, off+4+32)
	copy(resp, query[:off+4])
	queryFlags := binary.BigEndian.Uint16(resp[2:4])
	// QR | AA | RA，保留 RD。
	binary.BigEndian.PutUint16(resp[2:4], 0x8000|0x0400|0x0080|(queryFlags&0x0100))
	binary.BigEndian.PutUint16(resp[6:8], 1)  // ANCOUNT
	binary.BigEndian.PutUint16(resp[8:10], 0) // NSCOUNT
	binary.BigEndian.PutUint16(resp[10:12], 0)

	resp = append(resp, 0xC0, 0x0C) // 指向问题段的名字
	resp = binary.BigEndian.AppendUint16(resp, qtype)
	resp = binary.BigEndian.AppendUint16(resp, 1) // class IN
	resp = binary.BigEndian.AppendUint32(resp, dnsTTL)
	if qtype == 1 {
		resp = binary.BigEndian.AppendUint16(resp, 4)
		resp = append(resp, 127, 0, 0, 1)
	} else {
		resp = binary.BigEndian.AppendUint16(resp, 16)
		resp = append(resp, net.ParseIP("::1").To16()...)
	}
	return resp, true, nil
}

func parseQName(msg []byte, off int) (string, int, error) {
	var labels []string
	total := 1
	for {
		if off >= len(msg) {
			return "", 0, fmt.Errorf("DNS 名字越界")
		}
		length := int(msg[off])
		if length == 0 {
			off++
			break
		}
		if length&0xC0 != 0 {
			return "", 0, fmt.Errorf("DNS 查询不支持压缩指针")
		}
		off++
		if off+length > len(msg) {
			return "", 0, fmt.Errorf("DNS 标签截断")
		}
		total += 1 + length
		if total > 255 {
			return "", 0, fmt.Errorf("DNS 名字过长")
		}
		labels = append(labels, string(msg[off:off+length]))
		off += length
	}
	if len(labels) == 0 {
		return "", 0, fmt.Errorf("DNS 名字为空")
	}
	return strings.ToLower(strings.Join(labels, ".")), off, nil
}
