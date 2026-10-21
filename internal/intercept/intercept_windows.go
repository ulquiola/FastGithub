//go:build windows

package intercept

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"fastgithub-go/internal/config"
)

const windivertLayerNetwork = 0

type winDivert struct {
	dll      *syscall.DLL
	open     *syscall.Proc
	recv     *syscall.Proc
	send     *syscall.Proc
	close    *syscall.Proc
	checksum *syscall.Proc
}

// Available 报告 WinDivert DLL 是否真的可以加载；驱动能否打开由 Start 决定。
func Available() bool {
	w, err := loadWinDivert("")
	if err != nil {
		return false
	}
	_ = w.dll.Release()
	return true
}

func loadWinDivert(driverDir string) (*winDivert, error) {
	candidates := []string{}
	if driverDir != "" {
		candidates = append(candidates, filepath.Join(driverDir, "WinDivert.dll"))
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "WinDivert.dll"),
			filepath.Join(dir, "windivert", "WinDivert.dll"))
	}
	candidates = append(candidates, "WinDivert.dll")

	var lastErr error
	for _, path := range candidates {
		if path != "WinDivert.dll" {
			if _, err := os.Stat(path); err != nil {
				continue
			}
			sysPath := filepath.Join(filepath.Dir(path), "WinDivert64.sys")
			if _, err := os.Stat(sysPath); err != nil {
				lastErr = fmt.Errorf("缺少 %s: %w", sysPath, err)
				continue
			}
		}
		dll, err := syscall.LoadDLL(path)
		if err != nil {
			lastErr = err
			continue
		}
		w := &winDivert{dll: dll}
		procs := []struct {
			name string
			dst  **syscall.Proc
		}{
			{"WinDivertOpen", &w.open},
			{"WinDivertRecv", &w.recv},
			{"WinDivertSend", &w.send},
			{"WinDivertClose", &w.close},
			{"WinDivertHelperCalcChecksums", &w.checksum},
		}
		ok := true
		for _, p := range procs {
			proc, err := dll.FindProc(p.name)
			if err != nil {
				lastErr = fmt.Errorf("%s: %w", p.name, err)
				ok = false
				break
			}
			*p.dst = proc
		}
		if ok {
			return w, nil
		}
		dll.Release()
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("未找到 WinDivert.dll")
	}
	return nil, lastErr
}

func (w *winDivert) openHandle(filter string) (syscall.Handle, error) {
	filterPtr, err := syscall.BytePtrFromString(filter)
	if err != nil {
		return 0, err
	}
	r, _, errno := w.open.Call(uintptr(unsafe.Pointer(filterPtr)), windivertLayerNetwork, 0, 0)
	if r == uintptr(syscall.InvalidHandle) {
		return 0, fmt.Errorf("WinDivertOpen(%s): %w", filter, errno)
	}
	return syscall.Handle(r), nil
}

func (w *winDivert) recvPacket(handle syscall.Handle, buf []byte, addr *Address) (int, error) {
	var received uint32
	r, _, errno := w.recv.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		uintptr(unsafe.Pointer(&received)),
		uintptr(unsafe.Pointer(addr)),
	)
	if r == 0 {
		return 0, errno
	}
	return int(received), nil
}

func (w *winDivert) sendPacket(handle syscall.Handle, packet []byte, addr *Address) error {
	if len(packet) == 0 {
		return nil
	}
	var sent uint32
	r, _, errno := w.send.Call(
		uintptr(handle),
		uintptr(unsafe.Pointer(&packet[0])),
		uintptr(len(packet)),
		uintptr(unsafe.Pointer(&sent)),
		uintptr(unsafe.Pointer(addr)),
	)
	if r == 0 {
		return errno
	}
	return nil
}

func (w *winDivert) calcChecksums(packet []byte, addr *Address) {
	if len(packet) == 0 {
		return
	}
	w.checksum.Call(
		uintptr(unsafe.Pointer(&packet[0])),
		uintptr(len(packet)),
		uintptr(unsafe.Pointer(addr)),
		0,
	)
}

func (w *winDivert) closeHandle(handle syscall.Handle) {
	w.close.Call(uintptr(handle))
}

// Start 打开 DNS 与 TCP 两个 WinDivert 句柄并启动处理循环。
func Start(ctx context.Context, cfg Config) (*Handle, error) {
	w, err := loadWinDivert(cfg.DriverDir)
	if err != nil {
		return nil, err
	}
	preferredToActual := make(map[int]int, len(cfg.PortMap))
	for actual, preferred := range cfg.PortMap {
		preferredToActual[preferred] = actual
	}
	tcpFilter := TCPFilter(cfg.PortMap)

	dnsHandle, err := w.openHandle("udp.DstPort == 53")
	if err != nil {
		return nil, err
	}
	tcpHandle, err := w.openHandle(tcpFilter)
	if err != nil {
		w.closeHandle(dnsHandle)
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	go w.loopDNS(ctx, dnsHandle, cfg.Store, !cfg.DisableAAAA)
	go w.loopTCP(ctx, tcpHandle, cfg.PortMap, preferredToActual)
	return &Handle{
		cancel: cancel,
		closeFn: func() {
			w.closeHandle(dnsHandle)
			w.closeHandle(tcpHandle)
		},
	}, nil
}

func (w *winDivert) loopDNS(ctx context.Context, handle syscall.Handle, store *config.Store, allowAAAA bool) {
	buf := make([]byte, 65535)
	for ctx.Err() == nil {
		var addr Address
		n, err := w.recvPacket(handle, buf, &addr)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		packet := append([]byte(nil), buf[:n]...)
		pkt, err := ParsePacket(packet)
		if err != nil || pkt.Protocol != protoUDP {
			w.sendPacket(handle, packet, &addr)
			continue
		}
		payload, matched, err := DNSResponse(pkt.Payload, store, allowAAAA)
		if err != nil || !matched {
			w.sendPacket(handle, packet, &addr)
			continue
		}
		resp, err := RebuildUDP(packet, payload)
		if err != nil {
			w.sendPacket(handle, packet, &addr)
			continue
		}
		respAddr := addr
		respAddr.SetOutbound(false)
		respAddr.SetImpostor(true)
		w.sendPacket(handle, resp, &respAddr)
	}
}

func (w *winDivert) loopTCP(ctx context.Context, handle syscall.Handle, actualToPreferred, preferredToActual map[int]int) {
	buf := make([]byte, 65535)
	for ctx.Err() == nil {
		var addr Address
		n, err := w.recvPacket(handle, buf, &addr)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			continue
		}
		packet := append([]byte(nil), buf[:n]...)
		pkt, err := ParsePacket(packet)
		if err != nil || pkt.Protocol != protoTCP {
			w.sendPacket(handle, packet, &addr)
			continue
		}
		src, dst, changed := RemapTCPPorts(pkt.SrcPort, pkt.DstPort, actualToPreferred, preferredToActual)
		if changed {
			pkt.SetSrcPort(src)
			pkt.SetDstPort(dst)
		}
		if changed {
			addr.SetImpostor(true)
			if err := RecalculateChecksums(pkt.Raw); err != nil {
				w.sendPacket(handle, packet, &addr)
				continue
			}
			w.calcChecksums(pkt.Raw, &addr)
		}
		w.sendPacket(handle, pkt.Raw, &addr)
	}
}
