package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
)

func newUpdateCmd() *cobra.Command {
	var (
		component string
		checkOnly bool
		reboot    bool
	)
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and apply OS/k0s updates, then handle the reboot",
		Long: `Determines availability from systemd-sysupdate and applies the
update, honoring the node's reboot strategy (manual|staged|direct).

  apxctl update --check          # plan only
  apxctl update --component=os   # only the OS axis
  apxctl update --reboot         # reboot per the configured strategy`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			c, err := newClient()
			if err != nil {
				return err
			}
			defer c.Close()

			stream, err := c.Machine.Update(ctx, &apxv1.UpdateRequest{
				Component: parseComponent(component),
				CheckOnly: checkOnly,
				Reboot:    reboot,
			})
			if err != nil {
				return fmt.Errorf("update: %w", err)
			}
			for {
				resp, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					return nil
				}
				if err != nil {
					if errors.Is(err, context.Canceled) {
						return nil
					}
					return fmt.Errorf("update: %w", err)
				}
				switch v := resp.Payload.(type) {
				case *apxv1.UpdateResponse_Progress:
					fmt.Fprintln(cmd.OutOrStdout(), v.Progress)
				case *apxv1.UpdateResponse_Error:
					return errors.New(v.Error)
				}
			}
		},
	}
	cmd.Flags().StringVar(&component, "component", "all", "component(s) to update: all|os|k0s")
	cmd.Flags().BoolVar(&checkOnly, "check", false, "check for updates without applying")
	cmd.Flags().BoolVar(&reboot, "reboot", false, "reboot after staging, per the configured strategy")
	return cmd
}

func parseComponent(s string) apxv1.UpdateRequest_Component {
	switch s {
	case "os":
		return apxv1.UpdateRequest_COMPONENT_OS
	case "k0s":
		return apxv1.UpdateRequest_COMPONENT_K0S
	default:
		return apxv1.UpdateRequest_COMPONENT_ALL
	}
}

func newRollbackCmd() *cobra.Command {
	var reboot bool
	cmd := &cobra.Command{
		Use:   "rollback",
		Short: "Boot the previous A/B slot next time (optionally rebooting now)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			defer c.Close()

			ctx, cancel := context.WithTimeout(cmd.Context(), 60*time.Second)
			defer cancel()

			resp, err := c.Machine.Rollback(ctx, &apxv1.RollbackRequest{Reboot: reboot})
			if err != nil {
				return fmt.Errorf("rollback: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), resp.Message)
			return nil
		},
	}
	cmd.Flags().BoolVar(&reboot, "reboot", false, "reboot immediately into the previous slot")
	return cmd
}
