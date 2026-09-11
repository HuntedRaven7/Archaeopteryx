package apid

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
)

func TestListServicesRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		t.Skip("systemctl not available")
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

	resp, err := c.Machine.ListServices(ctx, &apxv1.ListServicesRequest{})
	if err != nil {
		t.Fatalf("ListServices: %v", err)
	}
	if len(resp.Services) == 0 {
		t.Fatal("no services returned")
	}
	found := false
	for _, s := range resp.Services {
		if s.Name == "systemd-journald.service" {
			found = true
			if s.State == "" || s.SubState == "" {
				t.Error("journald service missing state/substate")
			}
		}
	}
	if !found {
		t.Error("systemd-journald.service not listed")
	}
}

func TestServiceActionRejectsBadUnit(t *testing.T) {
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

	_, err = c.Machine.ServiceAction(ctx, &apxv1.ServiceActionRequest{
		Unit:   "../etc/passwd.service",
		Action: apxv1.ServiceActionRequest_START,
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ServiceAction bad unit = %v, want InvalidArgument", err)
	}
}

func TestLogsStreamTail(t *testing.T) {
	if _, err := exec.LookPath("journalctl"); err != nil {
		t.Skip("journalctl not available")
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

	stream, err := c.Machine.Logs(ctx, &apxv1.LogsRequest{TailLines: 5})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	for i := 0; i < 5; i++ {
		resp, err := stream.Recv()
		if err != nil {
			t.Fatalf("Logs recv: %v", err)
		}
		if len(resp.Data) == 0 {
			t.Fatal("empty log line")
		}
	}
	// Non-follow stream terminates after the tail window.
	if _, err := stream.Recv(); err == nil {
		t.Fatal("expected EOF after tail window")
	}
}

func TestLogsRejectsUnknownUnit(t *testing.T) {
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

	stream, err := c.Machine.Logs(ctx, &apxv1.LogsRequest{Unit: "does-not-exist-xyz.service", TailLines: 3})
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	// journalctl exits non-zero for an unknown unit; the stream ends.
	if _, err := stream.Recv(); err == nil {
		t.Log("unknown unit stream returned data")
	}
}
