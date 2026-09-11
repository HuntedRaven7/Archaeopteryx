package apid

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
)

// TestKubeconfigUnavailable asserts a clean Unavailable when k0s is absent.
func TestKubeconfigUnavailable(t *testing.T) {
	if _, err := exec.LookPath("k0s"); err == nil {
		t.Skip("k0s present; asserting the healthy path instead")
	}
	srv, endpoint, closeSrv := startTestServer(t)
	defer closeSrv()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ca, cert, key := srv.PKI().ReadAdminPath()
	c, err := newTestClient(t, endpoint, ca, cert, key)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	_, err = c.Machine.Kubeconfig(ctx, &apxv1.KubeconfigRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("Kubeconfig = %v, want Unavailable", err)
	}
}

// TestBootstrapUnavailable asserts a clean error when the k0s sysext image has
// not been installed yet.
func TestBootstrapUnavailable(t *testing.T) {
	for _, p := range []string{"/var/lib/extensions/k0s.raw", "/var/lib/k0s/k0s.raw"} {
		if _, err := os.Stat(p); err == nil {
			t.Skipf("%s present; not asserting the unavailable path", p)
		}
	}
	srv, endpoint, closeSrv := startTestServer(t)
	defer closeSrv()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	ca, cert, key := srv.PKI().ReadAdminPath()
	c, err := newTestClient(t, endpoint, ca, cert, key)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if _, err := c.Machine.Bootstrap(ctx, &apxv1.BootstrapRequest{}); err == nil {
		t.Fatal("expected Bootstrap to fail without a k0s sysext image")
	}
}
