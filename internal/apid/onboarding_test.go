package apid

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
	"github.com/HuntedRaven7/Archaeopteryx/internal/config"
	"github.com/HuntedRaven7/Archaeopteryx/pkg/client"
)

// TestOnboardingFlow exercises the full M2 maintenance-mode bootstrap:
// unconfigured node -> apply-config with onboarding token -> trust anchor swap
// -> old bootstrap admin rejected, generated bundle accepted.
func TestOnboardingFlow(t *testing.T) {
	srv, endpoint, closeSrv := startTestServer(t)
	defer closeSrv()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Operator generates the node config offline for this endpoint.
	gen, err := config.GenerateCluster("factory", endpoint, "")
	if err != nil {
		t.Fatalf("GenerateCluster: %v", err)
	}
	var cfg config.MachineConfig
	if err := yaml.Unmarshal(gen.ApxConfig, &cfg); err != nil {
		t.Fatalf("unmarshal generated config: %v", err)
	}

	// Bootstrap admin identity on the node.
	ca, cert, key := srv.PKI().ReadAdminPath()
	bootstrap, err := client.New(client.Options{Endpoint: endpoint, CA: ca, Cert: cert, Key: key})
	if err != nil {
		t.Fatalf("bootstrap client: %v", err)
	}
	defer bootstrap.Close()

	// Pre-apply: GetConfig reports not configured.
	if _, err := bootstrap.Machine.GetConfig(ctx, &apxv1.GetConfigRequest{}); status.Code(err) != codes.NotFound {
		t.Fatalf("GetConfig pre-apply = %v, want NotFound", err)
	}

	token := srv.MaintenanceToken()
	if token == "" {
		t.Fatal("expected an onboarding token in maintenance mode")
	}

	// Wrong token is rejected and the node stays unconfigured.
	_, err = bootstrap.Machine.ApplyConfig(ctx, &apxv1.ApplyConfigRequest{
		Config:           string(gen.ApxConfig),
		MaintenanceToken: "not-the-token",
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("ApplyConfig wrong token = %v, want PermissionDenied", err)
	}
	if srv.IsConfigured() {
		t.Fatal("node configured after rejected apply")
	}

	// Correct token installs the config.
	resp, err := bootstrap.Machine.ApplyConfig(ctx, &apxv1.ApplyConfigRequest{
		Config:           string(gen.ApxConfig),
		MaintenanceToken: token,
	})
	if err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	if resp.Message == "" {
		t.Error("expected a human message from ApplyConfig")
	}
	if !srv.IsConfigured() {
		t.Fatal("node did not become configured")
	}

	// Maintenance material removed.
	if _, err := os.Stat(filepath.Join(srv.StateDir(), "onboarding-token")); !os.IsNotExist(err) {
		t.Error("onboarding token still present after apply")
	}
	if _, err := os.Stat(filepath.Join(srv.StateDir(), "ca.key")); !os.IsNotExist(err) {
		t.Error("bootstrap ca.key still present after apply")
	}

	// Applying again is rejected.
	_, err = bootstrap.Machine.ApplyConfig(ctx, &apxv1.ApplyConfigRequest{
		Config:           string(gen.ApxConfig),
		MaintenanceToken: token,
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("second ApplyConfig = %v, want AlreadyExists", err)
	}

	// A fresh client presenting the bootstrap identity — signed by the OLD CA —
	// must be rejected now that the trust anchor has swapped.
	boot2, err := client.New(client.Options{Endpoint: endpoint, CA: ca, Cert: cert, Key: key})
	if err != nil {
		t.Fatalf("bootstrap re-dial: %v", err)
	}
	defer boot2.Close()
	if _, err := boot2.Machine.Version(ctx, &apxv1.VersionRequest{}); err == nil {
		t.Fatal("bootstrap client still accepted after CA swap")
	}

	// The operator's generated bundle works against the new trust anchor.
	bd, err := config.ParseClientBundle(gen.ClientBundle)
	if err != nil {
		t.Fatalf("parse bundle: %v", err)
	}
	b := bd.First()
	if b == nil {
		t.Fatal("no bundle context")
	}
	dir := t.TempDir()
	writeFile := func(name, data string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return p
	}
	op, err := client.New(client.Options{
		Endpoint: endpoint,
		CA:       writeFile("ca.crt", b.CA),
		Cert:     writeFile("admin.crt", b.Cert),
		Key:      writeFile("admin.key", b.Key),
	})
	if err != nil {
		t.Fatalf("operator client: %v", err)
	}
	defer op.Close()

	if _, err := op.Machine.Version(ctx, &apxv1.VersionRequest{}); err != nil {
		t.Fatalf("operator Version after apply: %v", err)
	}

	// GetConfig returns the config with secrets masked.
	got, err := op.Machine.GetConfig(ctx, &apxv1.GetConfigRequest{})
	if err != nil {
		t.Fatalf("GetConfig after apply: %v", err)
	}
	if !strings.Contains(got.Config, "[REDACTED]") {
		t.Error("GetConfig did not redact key material")
	}
	if strings.Contains(got.Config, cfg.API.Key) {
		t.Error("GetConfig leaked the server private key")
	}
}
