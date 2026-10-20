package certs

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestLeafForVerifiesAgainstCA(t *testing.T) {
	a, err := LoadOrCreate(filepath.Join(t.TempDir(), "cacert"))
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	leaf, err := a.LeafFor("github.com")
	if err != nil {
		t.Fatalf("LeafFor: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(a.Certificate())
	if _, err := leaf.Leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		DNSName:   "github.com",
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		t.Fatalf("链验证失败: %v", err)
	}
	if leaf.Leaf.Subject.CommonName != "github.com" {
		t.Fatalf("CN = %q", leaf.Leaf.Subject.CommonName)
	}
	if string(leaf.Leaf.AuthorityKeyId) != string(a.Certificate().SubjectKeyId) {
		t.Fatal("AKI 应等于 CA 的 SKI")
	}
	if len(leaf.Leaf.SubjectKeyId) == 0 {
		t.Fatal("叶子证书应有 SKI")
	}
	if leaf.Leaf.NotAfter.After(a.Certificate().NotAfter) {
		t.Fatal("叶子有效期不能超过 CA")
	}
	pub, ok := leaf.Leaf.PublicKey.(*rsa.PublicKey)
	if !ok || pub.N.BitLen() != 2048 {
		t.Fatalf("叶子公钥 = %T", leaf.Leaf.PublicKey)
	}
	if !containsIP(leaf.Leaf.IPAddresses, net.ParseIP("127.0.0.1")) || !containsIP(leaf.Leaf.IPAddresses, net.ParseIP("::1")) {
		t.Fatalf("SAN IP = %v", leaf.Leaf.IPAddresses)
	}
	if len(leaf.Certificate) != 2 {
		t.Fatalf("证书链长度 = %d，期望 2", len(leaf.Certificate))
	}
}

func TestLeafForIPDomain(t *testing.T) {
	a, err := LoadOrCreate(filepath.Join(t.TempDir(), "cacert"))
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	leaf, err := a.LeafFor("127.0.0.1")
	if err != nil {
		t.Fatalf("LeafFor: %v", err)
	}
	count := 0
	for _, ip := range leaf.Leaf.IPAddresses {
		if ip.Equal(net.ParseIP("127.0.0.1")) {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("127.0.0.1 出现 %d 次: %v", count, leaf.Leaf.IPAddresses)
	}
	if !containsIP(leaf.Leaf.IPAddresses, net.ParseIP("::1")) {
		t.Fatalf("SAN IP = %v", leaf.Leaf.IPAddresses)
	}
}

func TestLeafForCaches(t *testing.T) {
	a, err := LoadOrCreate(filepath.Join(t.TempDir(), "cacert"))
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	first, err := a.LeafFor("github.com")
	if err != nil {
		t.Fatal(err)
	}
	second, err := a.LeafFor("github.com")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("同一域名应复用缓存证书")
	}
	other, err := a.LeafFor("api.github.com")
	if err != nil {
		t.Fatal(err)
	}
	if other == first {
		t.Fatal("不同域名不应复用")
	}
}

func TestLeafForRenewsExpiring(t *testing.T) {
	a, err := LoadOrCreate(filepath.Join(t.TempDir(), "cacert"))
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	first, err := a.LeafFor("github.com")
	if err != nil {
		t.Fatal(err)
	}
	// 把时钟拨到叶子过期前 1 小时，应重新签发。
	a.now = func() time.Time { return first.Leaf.NotAfter.Add(-time.Hour) }
	second, err := a.LeafFor("github.com")
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("临近过期的证书应重新签发")
	}
}

func TestLeafForConcurrent(t *testing.T) {
	a, err := LoadOrCreate(filepath.Join(t.TempDir(), "cacert"))
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := a.LeafFor("github.com"); err != nil {
				t.Errorf("LeafFor: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestLeafForEmptyDomain(t *testing.T) {
	a, err := LoadOrCreate(filepath.Join(t.TempDir(), "cacert"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.LeafFor(""); err == nil {
		t.Fatal("空域名应报错")
	}
}

func TestHostnameInSAN(t *testing.T) {
	a, err := LoadOrCreate(filepath.Join(t.TempDir(), "cacert"))
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := a.LeafFor("example.com")
	if err != nil {
		t.Fatal(err)
	}
	host, err := os.Hostname()
	if err != nil || host == "" || host == "example.com" {
		t.Skip("本机无可用主机名")
	}
	found := false
	for _, name := range leaf.Leaf.DNSNames {
		if name == host {
			found = true
		}
	}
	if !found {
		t.Fatalf("SAN 缺少主机名 %q: %v", host, leaf.Leaf.DNSNames)
	}
}

func TestLoadOrCreateRejectsNonCA(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cacert")
	a, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	// 用叶子证书覆盖 CA 文件，加载时应拒绝。
	leaf, err := a.LeafFor("example.com")
	if err != nil {
		t.Fatalf("LeafFor: %v", err)
	}
	pemData := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leaf.Certificate[0]})
	if err := os.WriteFile(a.CertPath(), pemData, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(dir); err == nil {
		t.Fatal("非 CA 证书应被拒绝")
	}
}
