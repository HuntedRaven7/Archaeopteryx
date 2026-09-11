package config

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"strings"
	"testing"
)

func TestGenerateClusterRoundTrip(t *testing.T) {
	gen, err := GenerateCluster("factory", "10.0.0.5", "node1")
	if err != nil {
		t.Fatalf("GenerateCluster: %v", err)
	}

	cfg, err := ParseMachineConfig(gen.ApxConfig)
	if err != nil {
		t.Fatalf("parse generated config: %v", err)
	}
	if len(cfg.API.Endpoints) != 1 || cfg.API.Endpoints[0] != "https://10.0.0.5:50000" {
		t.Errorf("endpoints = %v, want normalized https://10.0.0.5:50000", cfg.API.Endpoints)
	}
	if cfg.Hostname != "node1" {
		t.Errorf("hostname = %q, want node1", cfg.Hostname)
	}
	if cfg.API.CA == "" || cfg.API.Cert == "" || cfg.API.Key == "" {
		t.Fatal("config missing identity material")
	}
}

func TestGenerateClusterCertsValid(t *testing.T) {
	gen, err := GenerateCluster("factory", "node1.internal:50000", "")
	if err != nil {
		t.Fatalf("GenerateCluster: %v", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(gen.CA) {
		t.Fatal("invalid CA PEM")
	}

	// Server identity must verify under the CA and carry server auth.
	serverPair, err := tls.X509KeyPair(gen.ServerCert, gen.ServerKey)
	if err != nil {
		t.Fatalf("server keypair: %v", err)
	}
	serverLeaf, err := x509.ParseCertificate(serverPair.Certificate[0])
	if err != nil {
		t.Fatalf("parse server cert: %v", err)
	}
	if _, err := serverLeaf.Verify(x509.VerifyOptions{Roots: caPool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}); err != nil {
		t.Errorf("server cert not valid: %v", err)
	}
	found := false
	for _, d := range serverLeaf.DNSNames {
		if d == "node1.internal" {
			found = true
		}
	}
	if !found {
		t.Errorf("server cert DNSNames = %v, want node1.internal", serverLeaf.DNSNames)
	}

	// Admin identity must verify and carry client auth.
	adminPair, err := tls.X509KeyPair(gen.AdminCert, gen.AdminKey)
	if err != nil {
		t.Fatalf("admin keypair: %v", err)
	}
	adminLeaf, err := x509.ParseCertificate(adminPair.Certificate[0])
	if err != nil {
		t.Fatalf("parse admin cert: %v", err)
	}
	if _, err := adminLeaf.Verify(x509.VerifyOptions{Roots: caPool, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Errorf("admin cert not valid: %v", err)
	}

	// Certs must be far in the future, not expiring today.
	if serverLeaf.NotAfter.Before(serverLeaf.NotBefore.AddDate(300, 0, 0)) {
		t.Errorf("server cert TTL too short: %v to %v", serverLeaf.NotBefore, serverLeaf.NotAfter)
	}
}

func TestClientBundleRoundTrip(t *testing.T) {
	gen, err := GenerateCluster("factory", "10.0.0.5", "")
	if err != nil {
		t.Fatalf("GenerateCluster: %v", err)
	}

	bd, err := ParseClientBundle(gen.ClientBundle)
	if err != nil {
		t.Fatalf("parse bundle: %v", err)
	}
	ctx := bd.First()
	if ctx == nil {
		t.Fatal("bundle has no contexts")
	}
	if ctx.Endpoint != "https://10.0.0.5:50000" {
		t.Errorf("bundle endpoint = %q", ctx.Endpoint)
	}
	if !strings.Contains(ctx.Cert, "-----BEGIN CERTIFICATE-----") {
		t.Error("bundle cert is not PEM")
	}
}

func TestWriteArtifactsAndReload(t *testing.T) {
	gen, err := GenerateCluster("factory", "10.0.0.5", "")
	if err != nil {
		t.Fatalf("GenerateCluster: %v", err)
	}
	dir := t.TempDir()
	written, err := gen.WriteArtifacts(dir)
	if err != nil {
		t.Fatalf("WriteArtifacts: %v", err)
	}
	if len(written) != 5 {
		t.Fatalf("wrote %d files, want 5", len(written))
	}

	cfg, err := LoadMachineConfig(dir + "/apxconfig.yaml")
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if len(cfg.API.Endpoints) != 1 {
		t.Errorf("reloaded endpoints = %v", cfg.API.Endpoints)
	}

	red := cfg.Redacted()
	if strings.Contains(red, cfg.API.Key) {
		t.Error("Redacted leaked the private key")
	}
	if !strings.Contains(red, "[REDACTED]") {
		t.Error("Redacted did not mask material")
	}
	if _, err := os.Stat(dir + "/admin.key"); err != nil {
		t.Errorf("admin.key missing: %v", err)
	}
}

func TestParseMachineConfigValidation(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"empty", "", "unsupported config version"},
		{"bad version", "version: v1alpha9\n", "unsupported config version"},
		{"no endpoints", "version: v1alpha1\napi:\n  ca: x\n  cert: y\n  key: z\n", "endpoint"},
		{"no ca", "version: v1alpha1\napi:\n  endpoints: [a]\n  cert: y\n  key: z\n", "api.ca"},
		{"no server identity", "version: v1alpha1\napi:\n  endpoints: [a]\n  ca: x\n", "server identity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ParseMachineConfig([]byte(tc.yaml))
			if err == nil {
				t.Fatalf("expected error containing %q, got config %+v", tc.want, cfg)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want substring %q", err, tc.want)
			}
		})
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	if got := NormalizeEndpoint("node1.internal"); got != "https://node1.internal:50000" {
		t.Errorf("NormalizeEndpoint(node1.internal) = %q", got)
	}
	if got := NormalizeEndpoint("10.0.0.9:9999"); got != "https://10.0.0.9:9999" {
		t.Errorf("NormalizeEndpoint = %q", got)
	}
}
