package config

import (
	"crypto/tls"
	"crypto/x509"
	"testing"
)

func TestEnsurePKIGeneratesIdentities(t *testing.T) {
	dir := t.TempDir()

	pki, err := EnsurePKI(dir)
	if err != nil {
		t.Fatalf("EnsurePKI: %v", err)
	}

	ca, cert, key := pki.ReadAdminPath()
	for _, p := range []string{ca, cert, key, pki.Dir()} {
		if p == "" {
			t.Fatal("empty identity path")
		}
	}

	certPEM := pki.CAPEM()
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(certPEM) {
		t.Fatal("CA is not valid PEM")
	}

	pair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		t.Fatalf("load admin identity: %v", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatalf("parse admin cert: %v", err)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}); err != nil {
		t.Fatalf("admin cert does not verify against CA: %v", err)
	}

	second, err := EnsurePKI(dir)
	if err != nil {
		t.Fatalf("EnsurePKI (idempotent): %v", err)
	}
	if string(second.CAPEM()) != string(pki.CAPEM()) {
		t.Fatal("EnsurePKI regenerated the CA on second call")
	}
}

func TestOnboardingToken(t *testing.T) {
	dir := t.TempDir()
	t1, err := LoadOrCreateOnboardingToken(dir)
	if err != nil {
		t.Fatalf("first token: %v", err)
	}
	if len(t1) < 32 {
		t.Fatalf("token too short: %d", len(t1))
	}
	t2, err := LoadOrCreateOnboardingToken(dir)
	if err != nil {
		t.Fatalf("second token: %v", err)
	}
	if t1 != t2 {
		t.Fatal("token changed across calls")
	}
}
