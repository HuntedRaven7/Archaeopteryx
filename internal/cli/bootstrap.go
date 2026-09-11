package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
)

func newBootstrapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap",
		Short: "Stage and merge the k0s sysext, start the controller, wait for Ready",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			defer c.Close()

			ctx, cancel := context.WithTimeout(cmd.Context(), 6*time.Minute)
			defer cancel()

			resp, err := c.Machine.Bootstrap(ctx, &apxv1.BootstrapRequest{})
			if err != nil {
				return fmt.Errorf("bootstrap: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), resp.Message)
			return nil
		},
	}
}
