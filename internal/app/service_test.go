package app

import (
	"strings"
	"testing"
)

func TestSystemdUnit(t *testing.T) {
	unit := SystemdUnit("/opt/fastgithub/fastgithub")
	if !strings.Contains(unit, `ExecStart="/opt/fastgithub/fastgithub" run --path "/opt/fastgithub/fastgithub.toml"`) {
		t.Fatalf("unit = %s", unit)
	}
	if !strings.Contains(unit, "WorkingDirectory=/opt/fastgithub") || !strings.Contains(unit, "Restart=always") {
		t.Fatalf("unit = %s", unit)
	}
}

func TestLaunchdPlist(t *testing.T) {
	plist := LaunchdPlist("/opt/fastgithub/fastgithub")
	if !strings.Contains(plist, "<string>/opt/fastgithub/fastgithub</string>") || !strings.Contains(plist, "<string>run</string>") {
		t.Fatalf("plist = %s", plist)
	}
	if !strings.Contains(plist, "<string>/opt/fastgithub/fastgithub.toml</string>") || !strings.Contains(plist, "<key>KeepAlive</key>") {
		t.Fatalf("plist = %s", plist)
	}
}
