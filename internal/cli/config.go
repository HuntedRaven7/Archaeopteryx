package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect the applied machine configuration",
	}
	cmd.AddCommand(newConfigGetCmd())
	return cmd
}

func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get",
		Short: "Print the applied machine config (key material masked)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			defer c.Close()

			ctx, cancel := context.WithTimeout(cmd.Context(), 15*time.Second)
			defer cancel()

			resp, err := c.Machine.GetConfig(ctx, &apxv1.GetConfigRequest{})
			if err != nil {
				return fmt.Errorf("get config: %w", err)
			}
			fmt.Print(resp.Config)
			return nil
		},
	}
}
