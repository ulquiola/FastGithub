package macapp

import "testing"

func TestProxyTitle(t *testing.T) {
	if got := (MenuState{}).ProxyTitle(); got != "代理：未运行" {
		t.Fatalf("got %q", got)
	}
	if got := (MenuState{Running: true, ProxyAddr: "127.0.0.1:38457"}).ProxyTitle(); got != "代理：运行中 · 127.0.0.1:38457" {
		t.Fatalf("got %q", got)
	}
	if got := (MenuState{Error: "端口占用"}).ProxyTitle(); got != "代理：未运行（端口占用）" {
		t.Fatalf("got %q", got)
	}
}

func TestSystemProxyTitle(t *testing.T) {
	cases := []struct {
		state MenuState
		want  string
	}{
		{MenuState{SystemProxy: "enabled"}, "系统代理：已启用（点击关闭）"},
		{MenuState{SystemProxy: "disabled"}, "系统代理：未启用（点击启用）"},
		{MenuState{SystemProxy: "unsupported", SystemProxyNote: "默认路由为 VPN"}, "系统代理：不可用（VPN）"},
		{MenuState{SystemProxy: "unsupported"}, "系统代理：不可用"},
	}
	for _, c := range cases {
		if got := c.state.SystemProxyTitle(); got != c.want {
			t.Fatalf("got %q, want %q", got, c.want)
		}
	}
}

func TestCertTitle(t *testing.T) {
	if got := (MenuState{}).CertTitle(); got != "安装 CA" {
		t.Fatalf("got %q", got)
	}
	if got := (MenuState{CertInstalled: true}).CertTitle(); got != "卸载 CA" {
		t.Fatalf("got %q", got)
	}
}
