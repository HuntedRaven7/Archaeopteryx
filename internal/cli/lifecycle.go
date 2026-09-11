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

func newRebootCmd() *cobra.Command {
	var mode string
	cmd := &cobra.Command{
		Use:   "reboot",
		Short: "Reboot the node (graceful) or power it off",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			reqMode := apxv1.RebootRequest_MODE_GRACEFUL
			switch mode {
			case "graceful", "":
			case "poweroff":
				reqMode = apxv1.RebootRequest_MODE_POWEROFF
			default:
				return fmt.Errorf("unknown mode %q (graceful|poweroff)", mode)
			}
			c, err := newClient()
			if err != nil {
				return err
			}
			defer c.Close()

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			resp, err := c.Machine.Reboot(ctx, &apxv1.RebootRequest{Mode: reqMode})
			if err != nil {
				return fmt.Errorf("reboot: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), resp.Message)
			return nil
		},
	}
	cmd.Flags().StringVar(&mode, "mode", "graceful", "graceful|poweroff")
	return cmd
}

func newShutdownCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "shutdown",
		Short: "Power the node off",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			defer c.Close()

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			resp, err := c.Machine.Shutdown(ctx, &apxv1.ShutdownRequest{})
			if err != nil {
				return fmt.Errorf("shutdown: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), resp.Message)
			return nil
		},
	}
}

func newResetCmd() *cobra.Command {
	var wipe bool
	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Stop k0s and wipe its state, then reboot",
		Long: `Stops the k0s controller, runs k0s reset, removes state, and reboots.

  apxctl reset           # remove k0s cluster state, keep node identity
  apxctl reset --wipe    # full factory reset: also drop machine config + TLS
                         # identity so the node boots back into maintenance mode`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			defer c.Close()

			ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Minute)
			defer cancel()

			resp, err := c.Machine.Reset(ctx, &apxv1.ResetRequest{Wipe: wipe})
			if err != nil {
				return fmt.Errorf("reset: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), resp.Message)
			return nil
		},
	}
	cmd.Flags().BoolVar(&wipe, "wipe", false, "full factory reset (maintenance mode on next boot)")
	return cmd
}

func newEventsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "events",
		Short: "Stream machine, update, and k0s events",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			c, err := newClient()
			if err != nil {
				return err
			}
			defer c.Close()

			stream, err := c.Machine.Events(ctx, &apxv1.EventsRequest{})
			if err != nil {
				return fmt.Errorf("events: %w", err)
			}
			for {
				ev, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					return nil
				}
				if err != nil {
					if errors.Is(err, context.Canceled) {
						return nil
					}
					return fmt.Errorf("events: %w", err)
				}
				ts := time.Unix(0, ev.TimestampNs).Format(time.RFC3339)
				fmt.Fprintf(cmd.OutOrStdout(), "%s %-7s %s\n", ts, ev.Type, ev.Message)
			}
		},
	}
}
