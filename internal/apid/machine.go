package apid

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
	"github.com/HuntedRaven7/Archaeopteryx/internal/machine"
	"github.com/HuntedRaven7/Archaeopteryx/pkg/version"
)

// machineServer implements apx.v1.MachineService for the node.
type machineServer struct {
	apxv1.UnimplementedMachineServiceServer
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
