//go:build darwin && cgo

package main

import "C"

import (
	"encoding/json"
	"errors"
	"log"
	"os"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/macapp"
	"fastgithub-go/internal/sysproxy"
)

var (
	errNoUI     = errors.New("代理尚未就绪")
	errNoConfig = errors.New("配置未加载")
)

type statePayload struct {
	macapp.MenuState
	ProxyTitle       string `json:"proxyTitle"`
	SystemProxyTitle string `json:"systemProxyTitle"`
	CertTitle        string `json:"certTitle"`
}

func setError(err error) C.int {
	mu.Lock()
	if err != nil {
		lastErr = err.Error()
	} else {
		lastErr = ""
	}
	mu.Unlock()
	if err != nil {
		log.Printf("菜单操作失败: %v", err)
		return -1
	}
	return 0
}

//export fg_state_json
func fg_state_json() *C.char {
	mu.Lock()
	state := macapp.MenuState{
		Running:   running,
		ProxyAddr: proxyAddr,
		Error:     runErr,
	}
	port := 0
	if cfg != nil {
		port = cfg.Server.ProxyPort
	}
	authority := ca
	mu.Unlock()

	if port != 0 {
		if status, err := sysproxy.Get(port); err == nil {
			switch {
			case !status.Supported:
				state.SystemProxy = "unsupported"
				state.SystemProxyNote = status.Note
			case status.Enabled:
				state.SystemProxy = "enabled"
			default:
				state.SystemProxy = "disabled"
			}
		}
	}
	if authority != nil {
		if status, err := certs.Status(authority.CertPath()); err == nil {
			state.CertInstalled = status.Installed
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		state.Autostart = macapp.LaunchAgentEnabled(home)
	}
	payload := statePayload{MenuState: state}
	payload.ProxyTitle = state.ProxyTitle()
	payload.SystemProxyTitle = state.SystemProxyTitle()
	payload.CertTitle = state.CertTitle()
	data, _ := json.Marshal(payload)
	return C.CString(string(data))
}

//export fg_last_error
func fg_last_error() *C.char {
	mu.Lock()
	defer mu.Unlock()
	return C.CString(lastErr)
}

//export fg_open_ui
func fg_open_ui() C.int {
	mu.Lock()
	addr := uiAddr
	mu.Unlock()
	if addr == "" {
		return setError(errNoUI)
	}
	if err := execCommand("open", "http://"+addr+"/"); err != nil {
		return setError(err)
	}
	return 0
}

//export fg_toggle_proxy
func fg_toggle_proxy() C.int {
	mu.Lock()
	port := 0
	if cfg != nil {
		port = cfg.Server.ProxyPort
	}
	mu.Unlock()
	if port == 0 {
		return setError(errNoConfig)
	}
	status, err := sysproxy.Get(port)
	if err != nil {
		return setError(err)
	}
	if status.Supported && status.Enabled {
		return setError(sysproxy.Disable(port))
	}
	return setError(sysproxy.Enable(port))
}

//export fg_toggle_cert
func fg_toggle_cert() C.int {
	mu.Lock()
	authority := ca
	mu.Unlock()
	if authority == nil {
		return setError(errNoConfig)
	}
	status, err := certs.Status(authority.CertPath())
	if err != nil {
		return setError(err)
	}
	if status.Installed {
		return setError(certs.Uninstall(authority.CertPath()))
	}
	return setError(certs.Install(authority.CertPath()))
}

//export fg_toggle_autostart
func fg_toggle_autostart() C.int {
	home, err := os.UserHomeDir()
	if err != nil {
		return setError(err)
	}
	if macapp.LaunchAgentEnabled(home) {
		_ = execCommand("launchctl", macapp.BootoutArgs(os.Getuid())...)
		return setError(macapp.RemoveLaunchAgent(home))
	}
	exe, err := os.Executable()
	if err != nil {
		return setError(err)
	}
	if err := macapp.WriteLaunchAgent(home, exe); err != nil {
		return setError(err)
	}
	if err := execCommand("launchctl", macapp.BootstrapArgs(home, os.Getuid())...); err != nil {
		_ = macapp.RemoveLaunchAgent(home)
		return setError(err)
	}
	return 0
}
