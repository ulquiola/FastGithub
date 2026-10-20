package sysproxy

import (
	"errors"
	"strings"
	"testing"
)

const serviceOrder = `An asterisk (*) denotes that a network service is disabled.
(1) Thunderbolt Bridge
(Hardware Port: Thunderbolt Bridge, Device: bridge0)

(2) Wi-Fi
(Hardware Port: Wi-Fi, Device: en0)

(3) Shadowrocket
(Hardware Port: com.liguangming.Shadowrocket, Device: )
`

func fakeRun(outputs map[string]string, calls *[]string) runner {
	return func(name string, args ...string) ([]byte, error) {
		key := name + " " + strings.Join(args, " ")
		if calls != nil {
			*calls = append(*calls, key)
		}
		out, ok := outputs[key]
		if !ok {
			return nil, errors.New("unexpected command: " + key)
		}
		return []byte(out), nil
	}
}

func TestParseProxyConfig(t *testing.T) {
	cfg := parseProxyConfig("Enabled: Yes\nServer: 127.0.0.1\nPort: 38457\nAuthenticated Proxy Enabled: 0\n")
	if !cfg.enabled || cfg.server != "127.0.0.1" || cfg.port != 38457 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	off := parseProxyConfig("Enabled: No\nServer: 127.0.0.1\nPort: 7890\n")
	if off.enabled || off.port != 7890 {
		t.Fatalf("unexpected config: %+v", off)
	}
}

func TestRouteInterface(t *testing.T) {
	out := "   route to: default\ndestination: default\n    gateway: 192.168.124.1\n  interface: en0\n"
	if got := routeInterface(out); got != "en0" {
		t.Fatalf("routeInterface = %q", got)
	}
	if got := routeInterface("no interface here"); got != "" {
		t.Fatalf("routeInterface = %q", got)
	}
}

func TestServiceForDevice(t *testing.T) {
	if got := serviceForDevice(serviceOrder, "en0"); got != "Wi-Fi" {
		t.Fatalf("serviceForDevice = %q", got)
	}
	if got := serviceForDevice(serviceOrder, "en7"); got != "" {
		t.Fatalf("serviceForDevice = %q", got)
	}
}

func TestGetDarwin(t *testing.T) {
	base := map[string]string{
		"route -n get default":                  "  interface: en0\n",
		"networksetup -listnetworkserviceorder": serviceOrder,
		"networksetup -getwebproxy Wi-Fi":       "Enabled: Yes\nServer: 127.0.0.1\nPort: 38457\n",
		"networksetup -getsecurewebproxy Wi-Fi": "Enabled: Yes\nServer: 127.0.0.1\nPort: 38457\n",
	}
	status, err := get(38457, "darwin", fakeRun(base, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !status.Enabled || status.Service != "Wi-Fi" || status.Port != 38457 {
		t.Fatalf("unexpected status: %+v", status)
	}

	base["networksetup -getsecurewebproxy Wi-Fi"] = "Enabled: No\nServer: 127.0.0.1\nPort: 38457\n"
	status, err = get(38457, "darwin", fakeRun(base, nil))
	if err != nil {
		t.Fatal(err)
	}
	if status.Enabled {
		t.Fatalf("expected disabled: %+v", status)
	}
}

func TestEnableDarwin(t *testing.T) {
	var calls []string
	outputs := map[string]string{
		"route -n get default":                                  "  interface: en0\n",
		"networksetup -listnetworkserviceorder":                 serviceOrder,
		"networksetup -setwebproxy Wi-Fi 127.0.0.1 38457":       "",
		"networksetup -setsecurewebproxy Wi-Fi 127.0.0.1 38457": "",
	}
	if err := enable(38457, "darwin", fakeRun(outputs, &calls)); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 4 {
		t.Fatalf("calls = %v", calls)
	}
}

func TestDisableDarwin(t *testing.T) {
	outputs := map[string]string{
		"route -n get default":                           "  interface: en0\n",
		"networksetup -listnetworkserviceorder":          serviceOrder,
		"networksetup -getwebproxy Wi-Fi":                "Enabled: Yes\nServer: 127.0.0.1\nPort: 38457\n",
		"networksetup -setwebproxystate Wi-Fi off":       "",
		"networksetup -setsecurewebproxystate Wi-Fi off": "",
	}
	if err := disable(38457, "darwin", fakeRun(outputs, nil)); err != nil {
		t.Fatal(err)
	}

	foreign := map[string]string{
		"route -n get default":                  "  interface: en0\n",
		"networksetup -listnetworkserviceorder": serviceOrder,
		"networksetup -getwebproxy Wi-Fi":       "Enabled: Yes\nServer: 127.0.0.1\nPort: 7890\n",
	}
	if err := disable(38457, "darwin", fakeRun(foreign, nil)); err == nil {
		t.Fatal("expected error when another proxy is active")
	}
}

func TestUnsupportedPlatforms(t *testing.T) {
	if _, err := get(38457, "windows", nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("get windows err = %v", err)
	}
	if err := enable(38457, "linux", nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("enable linux err = %v", err)
	}
	if err := disable(38457, "windows", nil); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("disable windows err = %v", err)
	}
}
