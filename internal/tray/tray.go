// Package tray 提供 Windows 通知区域图标（托盘）以及跨平台的状态与菜单定义。
//
// Windows 之外的平台没有通知区域实现，Run/AutostartEnabled/SetAutostart 返回
// ErrUnsupported，CLI 会据此回退为控制台模式。
package tray

import (
	"errors"
	"strings"
)

var (
	// ErrUnsupported 表示当前平台不支持通知区域图标。
	ErrUnsupported = errors.New("当前平台不支持通知区域图标")
	// ErrAlreadyRunning 表示已有托盘实例持有单实例互斥体。
	ErrAlreadyRunning = errors.New("已有 FastGithub 托盘实例在运行")
)

// 菜单项 ID；值与 Windows 菜单命令 ID 一一对应。
const (
	IDOpenUI         = 1
	IDCopyProxy      = 2
	IDInstallCert    = 3
	IDInterceptState = 4
	IDAutostart      = 5
	IDQuit           = 6
)

// Item 是一个菜单项定义，跨平台可测。
type Item struct {
	ID        int
	Label     string
	Enabled   bool
	Checked   bool
	Separator bool
}

// Actions 是托盘菜单触发的回调；为 nil 时由平台实现提供默认行为。
type Actions struct {
	OpenUI      func()       // 打开 http://127.0.0.1:<uiPort>/
	CopyProxy   func() error // 复制 127.0.0.1:<proxyPort>
	InstallCert func() error // 以管理员身份运行 install-cert
	Quit        func()       // 请求退出（关闭代理）
}

// Options 是托盘运行所需的只读快照。
//
// Autostart 是 §2 之外新增的字段：菜单需要展示开机启动的勾选态，而该状态只能由
// CLI 从注册表读出后传入，MenuItems 因此保持纯函数（见计划 §5 TestMenuItems）。
//
// ConfigPath 是主配置文件（fastgithub.toml）的绝对路径。Windows 登录启动项没有
// 工作目录，必须把该路径写进命令行的 --path，否则 config.Discover 找不到配置。
type Options struct {
	Version          string
	ProxyAddr        string
	UIAddr           string
	ConfigPath       string
	InterceptEnabled bool
	Admin            bool
	Autostart        bool
	Actions          Actions
}

// StatusText 返回通知区域图标的提示文本，包含版本、代理地址与拦截状态。
func StatusText(o Options) string {
	version := o.Version
	if version == "" {
		version = "dev"
	}
	intercept := "透明拦截: 已关闭"
	if o.InterceptEnabled {
		intercept = "透明拦截: 已开启"
	}
	return "FastGithub " + version + "\n代理 " + o.ProxyAddr + "\n" + intercept
}

// MenuItems 返回右键菜单项定义；顺序与 ID 固定，Windows 实现额外插入分隔线。
func MenuItems(o Options) []Item {
	interceptLabel := "透明拦截: 已关闭"
	if o.InterceptEnabled {
		interceptLabel = "透明拦截: 已开启"
	}
	if !o.Admin {
		// 未提权时无法启动 WinDivert，拦截项只作状态展示。
		interceptLabel += "（需管理员）"
	}
	return []Item{
		{ID: IDOpenUI, Label: "打开界面", Enabled: true},
		{ID: IDCopyProxy, Label: "复制代理地址", Enabled: true},
		{ID: IDInstallCert, Label: "安装证书（UAC）", Enabled: true},
		// 拦截项只是状态展示：运行中切换需要重启代理，所以两种权限下都置灰，
		// 避免被误当成可切换的开关。
		{ID: IDInterceptState, Label: interceptLabel, Enabled: false, Checked: o.InterceptEnabled},
		{ID: IDAutostart, Label: "开机启动", Enabled: true, Checked: o.Autostart},
		{ID: IDQuit, Label: "退出", Enabled: true},
	}
}

// autostartCommand 返回写入 HKCU\...\Run 的命令行；exe 与配置路径含空格时必须加引号。
//
// 登录启动项由 Explorer 拉起时没有工作目录，因此必须带上主配置文件的绝对路径，
// 否则 config.Discover 只会找到 %AppData% 下的配置（通常不存在）。
func autostartCommand(exe, configPath string) string {
	command := `"` + strings.Trim(exe, `"`) + `" tray`
	if configPath == "" {
		return command
	}
	return command + ` --path "` + strings.Trim(configPath, `"`) + `"`
}

// uiURL 把监听地址转成可交给 ShellExecuteW 的 URL。
func uiURL(addr string) string {
	if addr == "" {
		return ""
	}
	if strings.Contains(addr, "://") {
		return addr
	}
	return "http://" + addr + "/"
}
