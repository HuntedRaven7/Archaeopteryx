package cli

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
)

func newServicesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "services",
		Short: "List systemd service unit states",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			defer c.Close()

			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			resp, err := c.Machine.ListServices(ctx, &apxv1.ListServicesRequest{})
			if err != nil {
				return fmt.Errorf("list services: %w", err)
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "UNIT\tLOAD\tACTIVE\tSUB\tDESCRIPTION")
			for _, s := range resp.Services {
				fmt.Fprintf(w, "%s\tloaded\t%s\t%s\t%s\n", s.Name, s.State, s.SubState, s.Description)
			}
			return w.Flush()
		},
	}
}

func newServiceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "service <start|stop|restart|reload> <unit>",
		Short: "Run a lifecycle action on a systemd service",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			var action apxv1.ServiceActionRequest_Action
			switch args[0] {
			case "start":
				action = apxv1.ServiceActionRequest_START
			case "stop":
				action = apxv1.ServiceActionRequest_STOP
			case "restart":
				action = apxv1.ServiceActionRequest_RESTART
			case "reload":
				action = apxv1.ServiceActionRequest_RELOAD
			default:
				return fmt.Errorf("unsupported service action %q (want start|stop|restart|reload)", args[0])
			}

			c, err := newClient()
			if err != nil {
				return err
			}
			defer c.Close()

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			resp, err := c.Machine.ServiceAction(ctx, &apxv1.ServiceActionRequest{
				Unit:   args[1],
				Action: action,
			})
			if err != nil {
				return fmt.Errorf("service action: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), resp.Message)
			return nil
		},
	}
}
