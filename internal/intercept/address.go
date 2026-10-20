package intercept

import "encoding/binary"

// AddressSize 是 WinDivert 2.2 WINDIVERT_ADDRESS 的 sizeof：
// Timestamp(0..7) + flags(8..11) + Reserved2(12..15) + union(16..79)。
const AddressSize = 80

// flags 位于 offset 8：Layer 0-7、Event 8-15、Sniffed 16、Outbound 17、
// Loopback 18、Impostor 19、IPv6 20、IP/TCP/UDPChecksum 21-23。
const (
	flagOutbound = 1 << 17
	flagLoopback = 1 << 18
	flagImpostor = 1 << 19
)

// Address 是 WinDivert 2.x 地址结构的原始 80 字节。
type Address [AddressSize]byte

func (a *Address) flags() uint32 { return binary.LittleEndian.Uint32(a[8:12]) }

func (a *Address) setFlags(flags uint32) { binary.LittleEndian.PutUint32(a[8:12], flags) }

func (a *Address) setFlag(mask uint32, value bool) {
	flags := a.flags()
	if value {
		flags |= mask
	} else {
		flags &^= mask
	}
	a.setFlags(flags)
}

func (a *Address) Layer() uint8 { return uint8(a.flags() & 0xFF) }

func (a *Address) SetLayer(layer uint8) {
	a.setFlags((a.flags() &^ 0xFF) | uint32(layer))
}

func (a *Address) Outbound() bool { return a.flags()&flagOutbound != 0 }

func (a *Address) SetOutbound(value bool) { a.setFlag(flagOutbound, value) }

func (a *Address) Loopback() bool { return a.flags()&flagLoopback != 0 }

func (a *Address) Impostor() bool { return a.flags()&flagImpostor != 0 }

func (a *Address) SetImpostor(value bool) { a.setFlag(flagImpostor, value) }

func (a *Address) NetworkIfIdx() uint32 { return binary.LittleEndian.Uint32(a[16:20]) }

func (a *Address) SetNetworkIfIdx(index uint32) {
	binary.LittleEndian.PutUint32(a[16:20], index)
}
