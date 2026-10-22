package dialer

import (
	"context"
	"net"
	"testing"

	"fastgithub-go/internal/config"
)

type sourceStub struct {
	lookupCalls int
	sourceCalls int
	source      string
}

func (s *sourceStub) Lookup(context.Context, string) ([]net.IP, error) {
	s.lookupCalls++
	return []net.IP{net.ParseIP("10.0.0.2")}, nil
}

func (s *sourceStub) LookupSource(_ context.Context, _, source string) ([]net.IP, error) {
	s.sourceCalls++
	s.source = source
	return []net.IP{net.ParseIP("10.0.0.1")}, nil
}

type plainStub struct{ calls int }

func (s *plainStub) Lookup(context.Context, string) ([]net.IP, error) {
	s.calls++
	return []net.IP{net.ParseIP("10.0.0.3")}, nil
}

func TestIPsUsesDomainSource(t *testing.T) {
	stub := &sourceStub{}
	d := New(nil, stub, nil)
	ips, err := d.ips(context.Background(), Target{
		Host: "linux.do", Port: 443,
		Domain: &config.Domain{DNS: "https://custom/query"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stub.sourceCalls != 1 || stub.lookupCalls != 0 {
		t.Fatalf("source=%d lookup=%d", stub.sourceCalls, stub.lookupCalls)
	}
	if stub.source != "https://custom/query" {
		t.Fatalf("source = %q", stub.source)
	}
	if len(ips) != 1 || ips[0].String() != "10.0.0.1" {
		t.Fatalf("ips = %v", ips)
	}
}

func TestIPsWithoutDomainSourceUsesLookup(t *testing.T) {
	stub := &sourceStub{}
	d := New(nil, stub, nil)
	ips, err := d.ips(context.Background(), Target{
		Host: "example.com", Port: 443, Domain: &config.Domain{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stub.lookupCalls != 1 || stub.sourceCalls != 0 {
		t.Fatalf("source=%d lookup=%d", stub.sourceCalls, stub.lookupCalls)
	}
	if len(ips) != 1 || ips[0].String() != "10.0.0.2" {
		t.Fatalf("ips = %v", ips)
	}
}

func TestIPsFallsBackWhenResolverLacksSourceSupport(t *testing.T) {
	stub := &plainStub{}
	d := New(nil, stub, nil)
	ips, err := d.ips(context.Background(), Target{
		Host: "linux.do", Port: 443,
		Domain: &config.Domain{DNS: "https://custom/query"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stub.calls != 1 {
		t.Fatalf("lookup calls = %d", stub.calls)
	}
	if len(ips) != 1 || ips[0].String() != "10.0.0.3" {
		t.Fatalf("ips = %v", ips)
	}
}
