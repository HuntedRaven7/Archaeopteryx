package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Display node status (host, boot slot, k0s)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			defer c.Close()

			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			st, err := c.Machine.GetStatus(ctx, &apxv1.GetStatusRequest{})
			if err != nil {
				return fmt.Errorf("query status: %w", err)
			}

			fmt.Printf("Host:\n")
			fmt.Printf("  hostname:      %s\n", st.Hostname)
			fmt.Printf("  os:            %s\n", st.OsPrettyName)
			fmt.Printf("  os id:         %s (%s)\n", st.OsId, st.OsName)
			fmt.Printf("  image:         %s\n", st.OsImageVersion)
			fmt.Printf("  kernel:        %s\n", st.KernelRelease)
			fmt.Printf("  boot id:       %s\n", st.BootId)
			fmt.Printf("  uptime:        %ds\n", st.UptimeSeconds)

			if st.BootSlot != nil {
				fmt.Printf("Boot slot:\n")
				if st.BootSlot.Current != "" {
					fmt.Printf("  current:       %s\n", st.BootSlot.Current)
				}
				fmt.Printf("  running:       %s\n", st.BootSlot.RunningVersion)
				fmt.Printf("  installed:     %s\n", st.BootSlot.InstalledVersion)
			}

			fmt.Printf("k0s:\n")
			if st.Kubernetes != nil {
				fmt.Printf("  state:         %s\n", stateName(st.Kubernetes.State))
				fmt.Printf("  sysext merged: %v\n", st.Kubernetes.SysextMerged)
				if st.Kubernetes.Version != "" {
					fmt.Printf("  version:       %s\n", st.Kubernetes.Version)
				}
				if st.Kubernetes.NodeName != "" {
					fmt.Printf("  node:          %s (%s)\n", st.Kubernetes.NodeName, st.Kubernetes.Role)
				}
				fmt.Printf("  ready:         %v\n", st.Kubernetes.Ready)
			}
			return nil
		},
	}
	return cmd
}

func stateName(s apxv1.KubernetesStatus_State) string {
	switch s {
	case apxv1.KubernetesStatus_DISABLED:
		return "disabled"
	case apxv1.KubernetesStatus_STOPPED:
		return "stopped"
	case apxv1.KubernetesStatus_ACTIVATING:
		return "activating"
	case apxv1.KubernetesStatus_ACTIVE:
		return "active"
	case apxv1.KubernetesStatus_FAILED:
		return "failed"
	default:
		return "unknown"
	}
}
