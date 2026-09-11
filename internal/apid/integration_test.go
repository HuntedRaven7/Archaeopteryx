package apid

import (
	"context"
	"net"
	"path/filepath"
	"testing"
	"time"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
	"github.com/HuntedRaven7/Archaeopteryx/pkg/client"
	"github.com/HuntedRaven7/Archaeopteryx/pkg/version"
)

// freePort grabs an ephemeral loopback port, releasing it immediately.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// newTestClient dials srv with the node's bootstrap admin identity.
func newTestClient(t *testing.T, endpoint, ca, cert, key string) (*client.Client, error) {
	t.Helper()
	return client.New(client.Options{Endpoint: endpoint, CA: ca, Cert: cert, Key: key})
}

func TestVersionRoundTrip(t *testing.T) {
	srv, endpoint, closeSrv := startTestServer(t)
	defer closeSrv()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := client.New(client.Options{
		Endpoint: endpoint,
		CA:       srv.PKI().CAPath(),
		Cert:     srv.PKI().Dir() + "/admin.crt",
		Key:      srv.PKI().Dir() + "/admin.key",
	})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer c.Close()

	got, err := c.Machine.Version(ctx, &apxv1.VersionRequest{})
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	want := version.Current()
	if got.Version != want.Version {
		t.Errorf("Version = %q, want %q", got.Version, want.Version)
	}
	if got.Platform != want.Platform {
		t.Errorf("Platform = %q, want %q", got.Platform, want.Platform)
	}
}

func TestGetStatusRoundTrip(t *testing.T) {
	srv, endpoint, closeSrv := startTestServer(t)
	defer closeSrv()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ca, cert, key := srv.PKI().ReadAdminPath()
	c, err := client.New(client.Options{Endpoint: endpoint, CA: ca, Cert: cert, Key: key})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer c.Close()

	st, err := c.Machine.GetStatus(ctx, &apxv1.GetStatusRequest{})
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if st.Hostname == "" {
		t.Error("expected non-empty hostname")
	}
	if st.Kubernetes == nil {
		t.Fatal("expected Kubernetes status")
	}
	if st.BootSlot == nil {
		t.Fatal("expected boot slot info")
	}
}

func TestUnauthenticatedRejected(t *testing.T) {
	endpoint, closeSrv := startTestServerEndpoint(t)
	defer closeSrv()

	// A rogue node PKI: its admin identity is NOT signed by the test server's
	// CA, so the server must refuse the handshake.
	rogue, err := New(Config{Listen: "127.0.0.1:0", StateDir: t.TempDir()})
	if err != nil {
		t.Fatalf("rogue pki: %v", err)
	}
	ca, cert, key := rogue.PKI().ReadAdminPath()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := client.New(client.Options{Endpoint: endpoint, CA: ca, Cert: cert, Key: key})
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	defer c.Close()

	if _, err := c.Machine.Version(ctx, &apxv1.VersionRequest{}); err == nil {
		t.Fatal("expected Version to fail with an untrusted client identity")
	}
}

func startTestServerEndpoint(t *testing.T) (string, func()) {
	t.Helper()
	_, endpoint, closeFn := startTestServer(t)
	return endpoint, closeFn
}

func startTestServer(t *testing.T) (*Server, string, func()) {
	t.Helper()
	endpoint := freePort(t)
	stateDir := t.TempDir()

	srv, err := New(Config{
		Listen:     endpoint,
		StateDir:   stateDir,
		ConfigPath: filepath.Join(stateDir, "config.yaml"),
	})
	if err != nil {
		t.Fatalf("apid.New: %v", err)
	}
	srv.Register()

	lis, err := net.Listen("tcp", endpoint)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = srv.grpc.Serve(lis)
	}()

	closeFn := func() {
		srv.grpc.Stop()
		<-done
	}
	return srv, endpoint, closeFn
}
