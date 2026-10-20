package certs

import (
	"crypto/rsa"
	"crypto/x509"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLoadOrCreateGeneratesCA(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cacert")
	a, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	cert := a.Certificate()
	if cert.Subject.CommonName != "FastGithub" || !cert.IsCA {
		t.Fatalf("CA = %+v", cert)
	}
	if cert.MaxPathLen != 1 || !cert.BasicConstraintsValid {
		t.Fatalf("BasicConstraints: valid=%v pathLen=%d", cert.BasicConstraintsValid, cert.MaxPathLen)
	}
	wantUsage := x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign | x509.KeyUsageCRLSign
	if cert.KeyUsage != wantUsage {
		t.Fatalf("KeyUsage = %v，期望 %v", cert.KeyUsage, wantUsage)
	}
	if len(cert.ExtKeyUsage) != 2 || cert.ExtKeyUsage[0] != x509.ExtKeyUsageServerAuth || cert.ExtKeyUsage[1] != x509.ExtKeyUsageClientAuth {
		t.Fatalf("ExtKeyUsage = %v", cert.ExtKeyUsage)
	}
	if len(cert.SubjectKeyId) == 0 || len(cert.DNSNames) != 1 || cert.DNSNames[0] != "FastGithub" {
		t.Fatalf("SKI/SAN = %v / %v", cert.SubjectKeyId, cert.DNSNames)
	}
	life := cert.NotAfter.Sub(cert.NotBefore)
	if life < 9*365*24*time.Hour || life > 11*365*24*time.Hour {
		t.Fatalf("CA 有效期 = %v", life)
	}
	pub, ok := cert.PublicKey.(*rsa.PublicKey)
	if !ok {
		t.Fatalf("公钥类型 = %T", cert.PublicKey)
	}
	if pub.N.BitLen() != 2048 {
		t.Fatalf("RSA 位数 = %d", pub.N.BitLen())
	}
	if _, err := os.Stat(a.KeyPath()); err != nil {
		t.Fatalf("私钥不存在: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(a.KeyPath())
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("私钥权限 = %v，期望 0600", info.Mode().Perm())
		}
		dirInfo, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		if dirInfo.Mode().Perm() != 0700 {
			t.Fatalf("目录权限 = %v，期望 0700", dirInfo.Mode().Perm())
		}
	}
}

func TestLoadOrCreateReusesCA(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cacert")
	first, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	second, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if first.Certificate().SerialNumber.Cmp(second.Certificate().SerialNumber) != 0 {
		t.Fatal("第二次加载不应重新生成 CA")
	}
	if string(first.CertPEM()) != string(second.CertPEM()) {
		t.Fatal("CertPEM 不一致")
	}
}

func TestLoadOrCreateLegacyCER(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cacert")
	a, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if err := os.Rename(a.CertPath(), filepath.Join(dir, "fastgithub.cer")); err != nil {
		t.Fatal(err)
	}
	legacy, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("旧版 .cer 应可加载: %v", err)
	}
	if legacy.Certificate().SerialNumber.Cmp(a.Certificate().SerialNumber) != 0 {
		t.Fatal("旧版证书序列号不一致")
	}
}

func TestLoadOrCreatePartialState(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cacert")
	a, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if err := os.Remove(a.KeyPath()); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreate(dir); err == nil {
		t.Fatal("只有证书没有私钥时应报错")
	}
}
