//go:build windows

package main

import (
	"context"
	"io"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	winServiceName = "fastgithub"

	serviceWin32OwnProcess              = 0x10
	serviceAcceptStop                   = 0x1
	serviceRunning                      = 0x4
	serviceStartPending                 = 0x2
	serviceStopPending                  = 0x3
	serviceStopped                      = 0x1
	serviceControlStop                  = 0x1
	serviceControlInterrogate           = 0x4
	errorFailedServiceControllerConnect = syscall.Errno(1063)
)

type serviceStatus struct {
	ServiceType             uint32
	CurrentState            uint32
	ControlsAccepted        uint32
	Win32ExitCode           uint32
	ServiceSpecificExitCode uint32
	CheckPoint              uint32
	WaitHint                uint32
}

type serviceTableEntry struct {
	ServiceName *uint16
	ServiceProc uintptr
}

var (
	advapi32                        = syscall.NewLazyDLL("advapi32.dll")
	procStartServiceCtrlDispatcherW = advapi32.NewProc("StartServiceCtrlDispatcherW")
	procRegisterServiceCtrlHandlerW = advapi32.NewProc("RegisterServiceCtrlHandlerW")
	procSetServiceStatus            = advapi32.NewProc("SetServiceStatus")

	statusHandle syscall.Handle
	stopOnce     sync.Once
	serviceStop  = make(chan struct{})
)

// runAsService 尝试作为 SCM 服务运行；交互运行时返回 handled=false。
func runAsService() (bool, int) {
	runtime.LockOSThread()
	name, err := syscall.UTF16PtrFromString(winServiceName)
	if err != nil {
		return false, 0
	}
	table := []serviceTableEntry{
		{ServiceName: name, ServiceProc: syscall.NewCallback(serviceMain)},
		{},
	}
	r, _, errno := procStartServiceCtrlDispatcherW.Call(uintptr(unsafe.Pointer(&table[0])))
	if r != 0 {
		return true, 0
	}
	if errno == errorFailedServiceControllerConnect {
		return false, 0
	}
	return true, 1
}

// serviceRunArgs 剥离 SCM 命令行里的 run 子命令，让 flag 解析从 --path 开始。
func serviceRunArgs(args []string) []string {
	if len(args) > 0 && args[0] == "run" {
		return args[1:]
	}
	return args
}

func serviceMain(argc uint32, argv **uint16) uintptr {
	name, _ := syscall.UTF16PtrFromString(winServiceName)
	handle, _, _ := procRegisterServiceCtrlHandlerW.Call(
		uintptr(unsafe.Pointer(name)),
		syscall.NewCallback(serviceCtrlHandler),
	)
	statusHandle = syscall.Handle(handle)
	setServiceStatus(serviceStartPending, 0)
	setServiceStatus(serviceRunning, serviceAcceptStop)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() {
		if err := runRunWithContext(ctx, serviceRunArgs(os.Args[1:]), io.Discard); err != nil {
			done <- 1
			return
		}
		done <- 0
	}()

	select {
	case code := <-done:
		cancel()
		setServiceStatus(serviceStopped, 0)
		return uintptr(code)
	case <-serviceStop:
		setServiceStatus(serviceStopPending, 0)
		cancel()
		<-done
		setServiceStatus(serviceStopped, 0)
		return 0
	}
}

func serviceCtrlHandler(control uint32) uintptr {
	switch control {
	case serviceControlStop:
		stopOnce.Do(func() { close(serviceStop) })
	case serviceControlInterrogate:
		setServiceStatus(serviceRunning, serviceAcceptStop)
	}
	return 0
}

func setServiceStatus(state, accepted uint32) {
	if statusHandle == 0 {
		return
	}
	status := serviceStatus{
		ServiceType:      serviceWin32OwnProcess,
		CurrentState:     state,
		ControlsAccepted: accepted,
	}
	procSetServiceStatus.Call(uintptr(statusHandle), uintptr(unsafe.Pointer(&status)))
}
