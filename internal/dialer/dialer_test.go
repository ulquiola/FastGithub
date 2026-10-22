package dialer

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"fastgithub-go/internal/certs"
	"fastgithub-go/internal/config"
	"fastgithub-go/internal/resolver"
)

type fakeResolver struct {
	ips []net.IP
	err error
}

func (f fakeResolver) Lookup(context.Context, string) ([]net.IP, error) {
	return f.ips, f.err
}

func testStore(t *testing.T, domain *config.Domain) *config.Store {
	t.Helper()
	cfg := config.Default()
	cfg.Domains = map[string]*config.Domain{"example.com": domain}
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func testAuthority(t *testing.T) *certs.Authority {
	t.Helper()
	ca, err := certs.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

func rootsFor(ca *certs.Authority) *x509.CertPool {
	pool := x509.NewCertPool()
	pool.AddCert(ca.Certificate())
	return pool
}

func startTLSServer(t *testing.T, cert tls.Certificate) (string, <-chan string) {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	sni := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		tlsConn := conn.(*tls.Conn)
		if err := tlsConn.Handshake(); err != nil {
			return
		}
		sni <- tlsConn.ConnectionState().ServerName
		tlsConn.Write([]byte("ok"))
	}()
	return ln.Addr().String(), sni
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, raw, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

func TestDialTLSSNI(t *testing.T) {
	ca := testAuthority(t)
	leaf, err := ca.LeafFor("example.com")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		conf *config.Domain
		want string
	}{
		{"domain", &config.Domain{IP: "127.0.0.1", TLSSNI: true}, "example.com"},
		{"ipaddress", &config.Domain{IP: "127.0.0.1", TLSSNI: true, TLSSNIPattern: "@ipaddress"}, ""},
		{"disabled", &config.Domain{IP: "127.0.0.1", TLSSNI: false}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr, sniCh := startTLSServer(t, *leaf)
			d := New(testStore(t, tc.conf), fakeResolver{}, rootsFor(ca))
			conn, err := d.DialTLS(context.Background(), Target{
				Host: "example.com", Port: portOf(t, addr), Domain: tc.conf,
			})
			if err != nil {
				t.Fatalf("DialTLS: %v", err)
			}
			defer conn.Close()
			if got := <-sniCh; got != tc.want {
				t.Fatalf("SNI = %q，期望 %q", got, tc.want)
			}
		})
	}
}

func TestDialTLSNameMismatch(t *testing.T) {
	ca := testAuthority(t)
	leaf, err := ca.LeafFor("other.com")
	if err != nil {
		t.Fatal(err)
	}
	conf := &config.Domain{IP: "127.0.0.1", TLSSNI: true}
	addr, _ := startTLSServer(t, *leaf)
	d := New(testStore(t, conf), fakeResolver{}, rootsFor(ca))
	if _, err := d.DialTLS(context.Background(), Target{Host: "example.com", Port: portOf(t, addr), Domain: conf}); err == nil || !strings.Contains(err.Error(), "名称不匹配") {
		t.Fatalf("err = %v，期望名称不匹配", err)
	}

	ignore := &config.Domain{IP: "127.0.0.1", TLSSNI: true, TLSIgnoreNameMismatch: true}
	addr2, _ := startTLSServer(t, *leaf)
	d2 := New(testStore(t, ignore), fakeResolver{}, rootsFor(ca))
	conn, err := d2.DialTLS(context.Background(), Target{Host: "example.com", Port: portOf(t, addr2), Domain: ignore})
	if err != nil {
		t.Fatalf("忽略名称不匹配时应成功: %v", err)
	}
	conn.Close()
}

func TestDialTLSRejectsUntrustedChain(t *testing.T) {
	ca := testAuthority(t)
	leaf, err := ca.LeafFor("example.com")
	if err != nil {
		t.Fatal(err)
	}
	conf := &config.Domain{IP: "127.0.0.1", TLSSNI: true, TLSIgnoreNameMismatch: true}
	addr, _ := startTLSServer(t, *leaf)
	d := New(testStore(t, conf), fakeResolver{}, x509.NewCertPool())
	if _, err := d.DialTLS(context.Background(), Target{Host: "example.com", Port: portOf(t, addr), Domain: conf}); err == nil {
		t.Fatal("不受信任的证书链应被拒绝")
	}
}

func TestIPOrder(t *testing.T) {
	conf := &config.Domain{IP: "3.3.3.3"}
	d := New(testStore(t, conf), fakeResolver{ips: []net.IP{
		net.ParseIP("2.2.2.2"), net.ParseIP("1.1.1.1"), net.ParseIP("2.2.2.2"),
	}}, nil)
	ips, err := d.ips(context.Background(), Target{Host: "example.com", Domain: conf})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"3.3.3.3", "2.2.2.2", "1.1.1.1"}
	if len(ips) != len(want) {
		t.Fatalf("ips = %v", ips)
	}
	for i, ip := range ips {
		if ip.String() != want[i] {
			t.Fatalf("ips = %v，期望 %v", ips, want)
		}
	}
}

func TestIPLiteralHostSkipsResolver(t *testing.T) {
	d := New(testStore(t, &config.Domain{}), fakeResolver{err: errors.New("不应调用")}, nil)
	ips, err := d.ips(context.Background(), Target{Host: "127.0.0.1", Domain: &config.Domain{}})
	if err != nil || len(ips) != 1 || !ips[0].Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("ips=%v err=%v", ips, err)
	}
}

func TestConfiguredIPWorksWhenResolverFails(t *testing.T) {
	conf := &config.Domain{IP: "1.2.3.4"}
	d := New(testStore(t, conf), fakeResolver{err: errors.New("boom")}, nil)
	ips, err := d.ips(context.Background(), Target{Host: "example.com", Domain: conf})
	if err != nil || len(ips) != 1 || !ips[0].Equal(net.ParseIP("1.2.3.4")) {
		t.Fatalf("ips=%v err=%v", ips, err)
	}
}

func TestMatchName(t *testing.T) {
	cert := &x509.Certificate{DNSNames: []string{"github.com", "*.githubusercontent.com"}}
	if !matchName(cert, "github.com") {
		t.Fatal("精确名称应匹配")
	}
	if !matchName(cert, "a.githubusercontent.com") {
		t.Fatal("通配名称应匹配")
	}
	if matchName(cert, "example.com") {
		t.Fatal("不相关名称不应匹配")
	}
	ipCert := &x509.Certificate{IPAddresses: []net.IP{net.ParseIP("1.2.3.4")}}
	if !matchName(ipCert, "1.2.3.4") {
		t.Fatal("IP SAN 应匹配")
	}
}

func TestDialTLSDefaultsSNIWhenNoDomain(t *testing.T) {
	ca := testAuthority(t)
	leaf, err := ca.LeafFor("example.com")
	if err != nil {
		t.Fatal(err)
	}
	addr, sniCh := startTLSServer(t, *leaf)
	d := New(testStore(t, &config.Domain{}), fakeResolver{ips: []net.IP{net.ParseIP("127.0.0.1")}}, rootsFor(ca))
	conn, err := d.DialTLS(context.Background(), Target{Host: "example.com", Port: portOf(t, addr)})
	if err != nil {
		t.Fatalf("DialTLS: %v", err)
	}
	defer conn.Close()
	if got := <-sniCh; got != "example.com" {
		t.Fatalf("无域名规则时应默认发送主机名，SNI = %q", got)
	}
}

func TestTimeoutFor(t *testing.T) {
	d := New(testStore(t, &config.Domain{}), fakeResolver{}, nil)
	if got := d.timeoutFor(Target{}); got != connectTimeout {
		t.Fatalf("默认超时 = %v", got)
	}
	conf := &config.Domain{Timeout: config.Duration(3 * time.Second)}
	if got := d.timeoutFor(Target{Domain: conf}); got != 3*time.Second {
		t.Fatalf("域名超时 = %v", got)
	}
}

func fallbackStore(t *testing.T, domain string, fallbacks map[string][]string) *config.Store {
	t.Helper()
	cfg := config.Default()
	cfg.Domains = map[string]*config.Domain{domain: {}}
	cfg.FallbackIPs = fallbacks
	store, err := config.NewStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func ipStrings(ips []net.IP) []string {
	out := make([]string, len(ips))
	for i, ip := range ips {
		out[i] = ip.String()
	}
	return out
}

func TestIPsIncludeFallbackIPs(t *testing.T) {
	store := fallbackStore(t, "example.com", map[string][]string{"example.com": {"9.9.9.9"}})
	d := New(store, fakeResolver{ips: []net.IP{net.ParseIP("1.1.1.1")}}, nil)
	ips, err := d.ips(context.Background(), Target{Host: "example.com", Domain: &config.Domain{}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1.1.1.1", "9.9.9.9"}
	if got := ipStrings(ips); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ips = %v，期望 %v", got, want)
	}
}

func TestIPsSkipFallbackWithStaticIP(t *testing.T) {
	store := fallbackStore(t, "example.com", map[string][]string{"example.com": {"9.9.9.9"}})
	conf := &config.Domain{IP: "3.3.3.3"}
	d := New(store, fakeResolver{ips: []net.IP{net.ParseIP("1.1.1.1")}}, nil)
	ips, err := d.ips(context.Background(), Target{Host: "example.com", Domain: conf})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"3.3.3.3", "1.1.1.1"}
	if got := ipStrings(ips); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("ips = %v，期望 %v", got, want)
	}
}

func TestIPsUseFallbackWhenResolverFails(t *testing.T) {
	store := fallbackStore(t, "example.com", map[string][]string{"example.com": {"9.9.9.9"}})
	d := New(store, fakeResolver{err: errors.New("boom")}, nil)
	ips, err := d.ips(context.Background(), Target{Host: "example.com", Domain: &config.Domain{}})
	if err != nil {
		t.Fatal(err)
	}
	if got := ipStrings(ips); len(got) != 1 || got[0] != "9.9.9.9" {
		t.Fatalf("ips = %v，期望 [9.9.9.9]", got)
	}
}

type recordingRacer struct {
	fakeResolver
	port  string
	order []net.IP
}

func (r *recordingRacer) Sort(_ context.Context, port string, _ []net.IP) []net.IP {
	r.port = port
	return r.order
}

func TestDialTCPUsesSortedCandidates(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	store := fallbackStore(t, "example.com", map[string][]string{"example.com": {"127.0.0.1"}})
	res := &recordingRacer{
		fakeResolver: fakeResolver{ips: []net.IP{net.ParseIP("192.0.2.1")}},
		order:        []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("192.0.2.1")},
	}
	d := New(store, res, nil)
	port := portOf(t, ln.Addr().String())
	conn, err := d.DialTCP(context.Background(), Target{Host: "example.com", Port: port, Domain: &config.Domain{}})
	if err != nil {
		t.Fatalf("DialTCP: %v", err)
	}
	conn.Close()
	if res.port != strconv.Itoa(port) {
		t.Fatalf("竞速端口 = %q，期望 %d", res.port, port)
	}
}

type realRacerResolver struct {
	ips   []net.IP
	racer *resolver.Resolver
}

func (r realRacerResolver) Lookup(context.Context, string) ([]net.IP, error) { return r.ips, nil }

func (r realRacerResolver) Sort(ctx context.Context, port string, ips []net.IP) []net.IP {
	return r.racer.Sort(ctx, port, ips)
}

func TestDialTCPUsesRealRacerFallback(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	store := fallbackStore(t, "example.com", map[string][]string{"example.com": {"127.0.0.1"}})
	res := realRacerResolver{
		ips:   []net.IP{net.ParseIP("127.0.0.2")},
		racer: resolver.New(config.DNS{}),
	}
	d := New(store, res, nil)
	conn, err := d.DialTCP(context.Background(), Target{
		Host: "example.com", Port: portOf(t, ln.Addr().String()), Domain: &config.Domain{},
	})
	if err != nil {
		t.Fatalf("DNS 答案不可达时应改用备用 IP: %v", err)
	}
	conn.Close()
}
