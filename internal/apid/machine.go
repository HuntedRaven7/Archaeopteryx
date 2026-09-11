package apid

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
	"github.com/HuntedRaven7/Archaeopteryx/internal/config"
	"github.com/HuntedRaven7/Archaeopteryx/internal/machine"
	"github.com/HuntedRaven7/Archaeopteryx/pkg/version"
)

// validateServerIdentity ensures the payload identity is a usable server
// certificate signed by the CA carried in the same config.
func validateServerIdentity(cfg *config.MachineConfig) error {
	pair, err := tls.X509KeyPair([]byte(cfg.API.Cert), []byte(cfg.API.Key))
	if err != nil {
		return fmt.Errorf("load cert/key: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return fmt.Errorf("parse cert: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM([]byte(cfg.API.CA)) {
		return fmt.Errorf("parse CA PEM")
	}
	if _, err := leaf.Verify(x509.VerifyOptions{
		Roots:     pool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}); err != nil {
		return fmt.Errorf("server cert not signed by the configured CA: %w", err)
	}
	return nil
}

// machineServer implements apx.v1.MachineService for the node.
type machineServer struct {
	apxv1.UnimplementedMachineServiceServer
	srv *Server
}

// Version reports the daemon build information.
func (m *machineServer) Version(ctx context.Context, req *apxv1.VersionRequest) (*apxv1.VersionResponse, error) {
	v := version.Current()
	return &apxv1.VersionResponse{
		Version:   v.Version,
		Commit:    v.Commit,
		BuildDate: v.BuildDate,
		GoVersion: v.GoVersion,
		Platform:  v.Platform,
	}, nil
}

// GetStatus gathers host, boot slot, and k0s state.
func (m *machineServer) GetStatus(ctx context.Context, req *apxv1.GetStatusRequest) (*apxv1.GetStatusResponse, error) {
	host, err := machine.CollectHost()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "host info: %v", err)
	}
	kube := machine.CollectKube()
	return &apxv1.GetStatusResponse{
		Hostname:       host.Hostname,
		OsId:           host.OSID,
		OsName:         host.OSName,
		OsPrettyName:   host.OSPrettyName,
		OsVersion:      host.OSVersion,
		OsImageVersion: host.OSImageVersion,
		KernelRelease:  host.KernelRelease,
		BootId:         host.BootID,
		UptimeSeconds:  int64(host.Uptime.Seconds()),
		BootSlot: &apxv1.SlotInfo{
			Current:          host.Slot,
			RunningVersion:   host.RunningVersion,
			InstalledVersion: host.InstalledVersion,
		},
		Kubernetes: &apxv1.KubernetesStatus{
			State:        apxv1.KubernetesStatus_State(int32(kube.State)),
			SysextMerged: kube.SysextMerged,
			Version:      kube.Version,
			NodeName:     kube.NodeName,
			Role:         kube.Role,
			Ready:        kube.Ready,
		},
	}, nil
}

// Logs streams journald entries as raw formatted lines.
func (m *machineServer) Logs(req *apxv1.LogsRequest, stream grpc.ServerStreamingServer[apxv1.LogsResponse]) error {
	opts := machine.JournalOptions{
		Unit:      req.Unit,
		Follow:    req.Follow,
		TailLines: int(req.TailLines),
		Since:     req.Since,
	}
	return machine.StreamJournal(stream.Context(), opts, func(e machine.JournalEntry) error {
		return stream.Send(&apxv1.LogsResponse{Data: []byte(machine.FormatJournalLine(e) + "\n")})
	})
}

// ListServices returns systemd service unit states.
func (m *machineServer) ListServices(ctx context.Context, req *apxv1.ListServicesRequest) (*apxv1.ListServicesResponse, error) {
	svcs, err := machine.ListServices()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, status.Errorf(codes.Unavailable, "systemctl not available: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "list services: %v", err)
	}
	resp := &apxv1.ListServicesResponse{}
	for _, s := range svcs {
		resp.Services = append(resp.Services, &apxv1.ServiceInfo{
			Name:            s.Name,
			State:           s.State,
			SubState:        s.SubState,
			Description:     s.Description,
			ActiveState:     s.ActiveState,
			ActiveEnterUsec: s.ActiveEnterUsec,
		})
	}
	return resp, nil
}

// ServiceAction applies a lifecycle verb to a systemd unit.
func (m *machineServer) ServiceAction(ctx context.Context, req *apxv1.ServiceActionRequest) (*apxv1.ServiceActionResponse, error) {
	var kind machine.ServiceActionKind
	switch req.Action {
	case apxv1.ServiceActionRequest_START:
		kind = machine.ActionStart
	case apxv1.ServiceActionRequest_STOP:
		kind = machine.ActionStop
	case apxv1.ServiceActionRequest_RESTART:
		kind = machine.ActionRestart
	case apxv1.ServiceActionRequest_RELOAD:
		kind = machine.ActionReload
	default:
		return nil, status.Errorf(codes.InvalidArgument, "unsupported action %v", req.Action)
	}
	if !machine.ValidUnit(req.Unit) {
		return nil, status.Errorf(codes.InvalidArgument, "invalid unit name %q", req.Unit)
	}
	msg, err := machine.ServiceAction(req.Unit, kind)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, status.Errorf(codes.Unavailable, "systemctl not available: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "service action: %v", err)
	}
	return &apxv1.ServiceActionResponse{Message: msg}, nil
}

// Bootstrap runs the k0s first-boot flow and returns when the node is Ready.
func (m *machineServer) Bootstrap(ctx context.Context, req *apxv1.BootstrapRequest) (*apxv1.BootstrapResponse, error) {
	timeout := 5 * time.Minute
	if dl, ok := ctx.Deadline(); ok {
		if d := time.Until(dl); d < timeout {
			timeout = d
		}
	}
	var steps []string
	err := machine.Bootstrap(ctx, timeout, func(s string) {
		steps = append(steps, s)
		slog.Info("bootstrap", "step", s)
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "bootstrap: %v", err)
	}
	return &apxv1.BootstrapResponse{Message: "k0s is running; node is Ready"}, nil
}

// Kubeconfig returns the admin kubeconfig for the k0s cluster.
func (m *machineServer) Kubeconfig(ctx context.Context, req *apxv1.KubeconfigRequest) (*apxv1.KubeconfigResponse, error) {
	out, err := machine.KubeconfigAdmin()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, status.Errorf(codes.Unavailable, "k0s not available: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "kubeconfig: %v", err)
	}
	return &apxv1.KubeconfigResponse{Kubeconfig: out}, nil
}

// Update runs the auto-determine update flow for the requested components.
func (m *machineServer) Update(req *apxv1.UpdateRequest, stream grpc.ServerStreamingServer[apxv1.UpdateResponse]) error {
	component := mapUpdateComponent(req.Component)
	strategy := "staged"
	if m.srv.IsConfigured() {
		if cfg, err := config.LoadMachineConfig(m.srv.ConfigPath()); err == nil {
			if s := cfg.Update.RebootStrategy; s != "" {
				strategy = s
			}
		}
	}
	opts := machine.UpdateOptions{
		Component:      component,
		CheckOnly:      req.CheckOnly,
		Reboot:         req.Reboot,
		RebootStrategy: strategy,
		Verify:         true,
	}
	_, err := machine.Update(stream.Context(), opts, func(line string) {
		_ = stream.Send(&apxv1.UpdateResponse{
			Payload: &apxv1.UpdateResponse_Progress{Progress: line},
		})
	})
	if err != nil {
		return status.Errorf(codes.Internal, "update: %v", err)
	}
	return nil
}

func mapUpdateComponent(c apxv1.UpdateRequest_Component) string {
	switch c {
	case apxv1.UpdateRequest_COMPONENT_OS:
		return "os"
	case apxv1.UpdateRequest_COMPONENT_K0S:
		return "k0s"
	default:
		return "all"
	}
}

// Rollback pins the previous systemd-boot slot, optionally rebooting.
func (m *machineServer) Rollback(ctx context.Context, req *apxv1.RollbackRequest) (*apxv1.RollbackResponse, error) {
	msg, err := machine.Rollback(ctx, req.Reboot)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "rollback: %v", err)
	}
	return &apxv1.RollbackResponse{Message: msg}, nil
}

// GetConfig returns the applied machine configuration with secrets masked, or
// NotFound when the node has not been configured yet.
func (m *machineServer) GetConfig(ctx context.Context, req *apxv1.GetConfigRequest) (*apxv1.GetConfigResponse, error) {
	cfg, err := config.LoadMachineConfig(m.srv.ConfigPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, status.Errorf(codes.NotFound, "node is not configured; use apply-config")
		}
		return nil, status.Errorf(codes.Internal, "read machine config: %v", err)
	}
	return &apxv1.GetConfigResponse{Config: cfg.Redacted()}, nil
}

// ApplyConfig installs a machine config while the node is unconfigured
// (maintenance mode), gated by the onboarding token when one is set.
func (m *machineServer) ApplyConfig(ctx context.Context, req *apxv1.ApplyConfigRequest) (*apxv1.ApplyConfigResponse, error) {
	if m.srv.IsConfigured() {
		return nil, status.Error(codes.AlreadyExists, "node is already configured")
	}

	cfg, err := config.ParseMachineConfig([]byte(req.Config))
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}

	// Verify the incoming server identity is usable and signed by the CA in
	// the config before we trust it over the wire.
	if err := validateServerIdentity(cfg); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "server identity: %v", err)
	}

	if tok := m.srv.MaintenanceToken(); tok != "" {
		if req.MaintenanceToken != tok {
			return nil, status.Error(codes.PermissionDenied, "invalid maintenance token")
		}
	}

	if err := m.srv.ApplyMachineConfig(cfg); err != nil {
		return nil, status.Errorf(codes.Internal, "apply config: %v", err)
	}

	return &apxv1.ApplyConfigResponse{
		Message: "machine config applied; node left maintenance mode (use the generated client bundle to reconnect)",
	}, nil
}
