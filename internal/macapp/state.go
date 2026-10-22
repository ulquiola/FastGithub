package macapp

// MenuState 是菜单栏需要展示的状态。
type MenuState struct {
	Running         bool   `json:"running"`
	ProxyAddr       string `json:"proxyAddr"`
	SystemProxy     string `json:"systemProxy"` // enabled | disabled | unsupported
	SystemProxyNote string `json:"systemProxyNote"`
	CertInstalled   bool   `json:"certInstalled"`
	Autostart       bool   `json:"autostart"`
	Error           string `json:"error"`
}

// ProxyTitle 返回代理状态行。
func (s MenuState) ProxyTitle() string {
	if !s.Running {
		if s.Error != "" {
			return "代理：未运行（" + s.Error + "）"
		}
		return "代理：未运行"
	}
	return "代理：运行中 · " + s.ProxyAddr
}

// SystemProxyTitle 返回系统代理菜单项文案。
func (s MenuState) SystemProxyTitle() string {
	switch s.SystemProxy {
	case "enabled":
		return "系统代理：已启用（点击关闭）"
	case "disabled":
		return "系统代理：未启用（点击启用）"
	default:
		if s.SystemProxyNote != "" {
			return "系统代理：不可用（VPN）"
		}
		return "系统代理：不可用"
	}
}

// CertTitle 返回证书菜单项文案。
func (s MenuState) CertTitle() string {
	if s.CertInstalled {
		return "卸载 CA"
	}
	return "安装 CA"
}
