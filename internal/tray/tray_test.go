package tray

import (
	"strings"
	"testing"
)

func TestStatusText(t *testing.T) {
	o := Options{
		Version:   "v9.8.7",
		ProxyAddr: "127.0.0.1:38457",
		UIAddr:    "127.0.0.1:38470",
	}
	got := StatusText(o)
	if !strings.Contains(got, "v9.8.7") {
		t.Fatalf("提示应包含版本号，实际 %q", got)
	}
	if !strings.Contains(got, "38457") {
		t.Fatalf("提示应包含代理端口，实际 %q", got)
	}
	if !strings.Contains(got, "拦截") || !strings.Contains(got, "已关闭") {
		t.Fatalf("拦截关闭时提示应含“拦截/已关闭”，实际 %q", got)
	}

	o.InterceptEnabled = true
	if got := StatusText(o); !strings.Contains(got, "已开启") {
		t.Fatalf("拦截开启时提示应含“已开启”，实际 %q", got)
	}

	// 未注入版本号时退回 dev，避免提示出现空洞。
	if got := StatusText(Options{}); !strings.Contains(got, "dev") {
		t.Fatalf("空版本号应退回 dev，实际 %q", got)
	}
}

func TestMenuItems(t *testing.T) {
	o := Options{
		Version:          "v9.8.7",
		ProxyAddr:        "127.0.0.1:38457",
		UIAddr:           "127.0.0.1:38470",
		InterceptEnabled: false,
		Admin:            false,
		Autostart:        true,
	}
	items := MenuItems(o)
	wantIDs := []int{IDOpenUI, IDCopyProxy, IDInstallCert, IDInterceptState, IDAutostart, IDQuit}
	if len(items) != len(wantIDs) {
		t.Fatalf("菜单项数 = %d，期望 %d：%+v", len(items), len(wantIDs), items)
	}
	for i, id := range wantIDs {
		if items[i].ID != id {
			t.Fatalf("第 %d 项 ID = %d，期望 %d", i, items[i].ID, id)
		}
		if items[i].Label == "" {
			t.Fatalf("第 %d 项缺少文案", i)
		}
		if items[i].Separator {
			t.Fatalf("第 %d 项不应是分隔符", i)
		}
	}

	intercept := items[3]
	if intercept.Enabled {
		t.Fatalf("非管理员时拦截项应禁用：%+v", intercept)
	}
	if !strings.Contains(intercept.Label, "需管理员") {
		t.Fatalf("非管理员时拦截项应带“需管理员”，实际 %q", intercept.Label)
	}
	if !items[4].Checked {
		t.Fatalf("开机启动已启用时该项应勾选：%+v", items[4])
	}

	o.Admin = true
	o.InterceptEnabled = true
	o.Autostart = false
	items = MenuItems(o)
	intercept = items[3]
	if intercept.Enabled {
		t.Fatalf("拦截项仅展示状态，管理员下也应禁用：%+v", intercept)
	}
	if !intercept.Checked {
		t.Fatalf("管理员且拦截开启时该项仍应勾选：%+v", intercept)
	}
	if strings.Contains(intercept.Label, "需管理员") {
		t.Fatalf("管理员时拦截项不应提示需管理员：%q", intercept.Label)
	}
	if items[4].Checked {
		t.Fatalf("开机启动未启用时该项不应勾选：%+v", items[4])
	}
}

func TestAutostartCommand(t *testing.T) {
	exe := `C:\Program Files\FastGithub\fastgithub.exe`
	configPath := `C:\Users\Test User\AppData\Roaming\fastgithub\fastgithub.toml`
	got := autostartCommand(exe, configPath)
	want := `"` + exe + `" tray --path "` + configPath + `"`
	if got != want {
		t.Fatalf("命令行 = %q，期望 %q", got, want)
	}
	// 明确挡住旧格式：登录启动没有工作目录，缺 --path 会读不到配置。
	if got == `"`+exe+`" tray` {
		t.Fatalf("命令行缺少 --path（旧格式）：%q", got)
	}
	if !strings.Contains(got, `--path "`+configPath+`"`) {
		t.Fatalf("--path 必须带引号包裹的绝对配置路径，实际 %q", got)
	}
}

func TestUIURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"127.0.0.1:38470", "http://127.0.0.1:38470/"},
		{"http://127.0.0.1:38470/", "http://127.0.0.1:38470/"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := uiURL(tc.in); got != tc.want {
			t.Fatalf("uiURL(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}
