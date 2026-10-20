package intercept

import (
	"encoding/binary"
	"testing"
)

func TestAddressFlags(t *testing.T) {
	var a Address
	// Layer 在低 8 位，Outbound=17、Loopback=18、Impostor=19。
	flags := uint32(3) | flagOutbound | flagLoopback | flagImpostor
	binary.LittleEndian.PutUint32(a[8:12], flags)

	if a.Layer() != 3 {
		t.Fatalf("Layer = %d", a.Layer())
	}
	if !a.Outbound() || !a.Loopback() || !a.Impostor() {
		t.Fatal("标志位应全部为真")
	}
	a.SetOutbound(false)
	a.SetImpostor(false)
	if a.Outbound() || a.Impostor() || !a.Loopback() {
		t.Fatal("清位错误")
	}
	a.SetLayer(0)
	if a.Layer() != 0 {
		t.Fatalf("Layer = %d", a.Layer())
	}
	if !a.Loopback() {
		t.Fatal("SetLayer 不应影响其他位")
	}
}

func TestAddressIfIdx(t *testing.T) {
	var a Address
	a.SetNetworkIfIdx(7)
	if a.NetworkIfIdx() != 7 {
		t.Fatalf("IfIdx = %d", a.NetworkIfIdx())
	}
}
