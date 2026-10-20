package config

import (
	"net"
	"strings"
	"testing"
)

func TestExpandSNI(t *testing.T) {
	if got := ExpandSNI("@domain", "github.com", nil); got != "github.com" {
		t.Fatalf("got %q", got)
	}
	if got := ExpandSNI("@IPADDRESS", "github.com", net.ParseIP("1.2.3.4")); got != "1.2.3.4" {
		t.Fatalf("got %q", got)
	}
	got := ExpandSNI("@random", "", nil)
	if got == "" || got == "@random" || strings.Contains(got, "@") {
		t.Fatalf("random 未展开: %q", got)
	}
}

func TestDomainSNIPattern(t *testing.T) {
	if got := (&Domain{TLSSNI: false, TLSSNIPattern: "@random"}).SNIPattern(); got != "" {
		t.Fatalf("tls_sni=false 应返回空，实际 %q", got)
	}
	if got := (&Domain{TLSSNI: true}).SNIPattern(); got != "@domain" {
		t.Fatalf("默认应为 @domain，实际 %q", got)
	}
	if got := (&Domain{TLSSNI: true, TLSSNIPattern: "@ipaddress"}).SNIPattern(); got != "@ipaddress" {
		t.Fatalf("got %q", got)
	}
}
