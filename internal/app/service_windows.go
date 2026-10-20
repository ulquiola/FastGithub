//go:build windows

package app

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

const (
	scManagerConnect       = 0x0001
	scManagerCreateService = 0x0002
	serviceAllAccess       = 0xF01FF
	serviceWin32OwnProcess = 0x00000010
	serviceAutoStart       = 0x00000002
	serviceErrorNormal     = 0x00000001
	serviceControlStop     = 0x00000001
	serviceQueryStatus     = 0x0004
	serviceStopped         = 1
	errorServiceExists     = syscall.Errno(1073)
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

var (
	advapi32               = syscall.NewLazyDLL("advapi32.dll")
	procOpenSCManager      = advapi32.NewProc("OpenSCManagerW")
	procOpenService        = advapi32.NewProc("OpenServiceW")
	procCreateService      = advapi32.NewProc("CreateServiceW")
	procDeleteService      = advapi32.NewProc("DeleteService")
	procStartService       = advapi32.NewProc("StartServiceW")
	procControlService     = advapi32.NewProc("ControlService")
	procQueryServiceStatus = advapi32.NewProc("QueryServiceStatus")
	procCloseServiceHandle = advapi32.NewProc("CloseServiceHandle")
)

func openSCManager(access uint32) (syscall.Handle, error) {
	handle, _, errno := procOpenSCManager.Call(0, 0, uintptr(access))
	if handle == 0 {
		return 0, fmt.Errorf("OpenSCManager: %w", errno)
	}
	return syscall.Handle(handle), nil
}

func openService(manager syscall.Handle, access uint32) (syscall.Handle, error) {
	name, err := syscall.UTF16PtrFromString(serviceName)
	if err != nil {
		return 0, err
	}
	handle, _, errno := procOpenService.Call(uintptr(manager), uintptr(unsafe.Pointer(name)), uintptr(access))
	if handle == 0 {
		return 0, fmt.Errorf("OpenService: %w", errno)
	}
	return syscall.Handle(handle), nil
}

func InstallService(exePath string) error {
	manager, err := openSCManager(scManagerConnect | scManagerCreateService)
	if err != nil {
		return err
	}
	defer procCloseServiceHandle.Call(uintptr(manager))

	name, err := syscall.UTF16PtrFromString(serviceName)
	if err != nil {
		return err
	}
	binaryPath, err := syscall.UTF16PtrFromString(serviceCommand(exePath))
	if err != nil {
		return err
	}
	handle, _, errno := procCreateService.Call(
		uintptr(manager),
		uintptr(unsafe.Pointer(name)),
		uintptr(unsafe.Pointer(name)),
		serviceAllAccess,
		serviceWin32OwnProcess,
		serviceAutoStart,
		serviceErrorNormal,
		uintptr(unsafe.Pointer(binaryPath)),
		0, 0, 0, 0, 0,
	)
	if handle == 0 {
		if errno == errorServiceExists {
			return fmt.Errorf("服务 %s 已存在", serviceName)
		}
		return fmt.Errorf("CreateService 失败（可能需要管理员权限）: %w", errno)
	}
	procCloseServiceHandle.Call(handle)
	return nil
}

func UninstallService() error {
	manager, err := openSCManager(scManagerConnect)
	if err != nil {
		return err
	}
	defer procCloseServiceHandle.Call(uintptr(manager))

	service, err := openService(manager, serviceAllAccess|serviceQueryStatus)
	if err != nil {
		return err
	}
	defer procCloseServiceHandle.Call(uintptr(service))

	_ = stopService(service)
	if r, _, errno := procDeleteService.Call(uintptr(service)); r == 0 {
		return fmt.Errorf("DeleteService 失败: %w", errno)
	}
	return nil
}

func StartService() error {
	manager, err := openSCManager(scManagerConnect)
	if err != nil {
		return err
	}
	defer procCloseServiceHandle.Call(uintptr(manager))
	service, err := openService(manager, serviceAllAccess)
	if err != nil {
		return err
	}
	defer procCloseServiceHandle.Call(uintptr(service))
	if r, _, errno := procStartService.Call(uintptr(service), 0, 0); r == 0 {
		return fmt.Errorf("StartService 失败: %w", errno)
	}
	return nil
}

func StopService() error {
	manager, err := openSCManager(scManagerConnect)
	if err != nil {
		return err
	}
	defer procCloseServiceHandle.Call(uintptr(manager))
	service, err := openService(manager, serviceAllAccess|serviceQueryStatus)
	if err != nil {
		return err
	}
	defer procCloseServiceHandle.Call(uintptr(service))
	return stopService(service)
}

func stopService(service syscall.Handle) error {
	var status serviceStatus
	if r, _, errno := procQueryServiceStatus.Call(uintptr(service), uintptr(unsafe.Pointer(&status))); r == 0 {
		return fmt.Errorf("QueryServiceStatus 失败: %w", errno)
	}
	if status.CurrentState == serviceStopped {
		return nil
	}
	if r, _, errno := procControlService.Call(uintptr(service), serviceControlStop, uintptr(unsafe.Pointer(&status))); r == 0 {
		return fmt.Errorf("ControlService(STOP) 失败: %w", errno)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		if r, _, _ := procQueryServiceStatus.Call(uintptr(service), uintptr(unsafe.Pointer(&status))); r != 0 && status.CurrentState == serviceStopped {
			return nil
		}
	}
	return fmt.Errorf("服务停止超时")
}
