package intercept

import (
	"strings"
	"testing"
)

func TestSelfTestText(t *testing.T) {
	text := SelfTestText()
	if !strings.Contains(text, "Layer=0") || !strings.Contains(text, "Outbound=true") {
		t.Fatalf("selftest = %s", text)
	}
	if !strings.Contains(text, "github.com A -> 127.0.0.1") {
		t.Fatalf("selftest 未解析 DNS: %s", text)
	}
}
