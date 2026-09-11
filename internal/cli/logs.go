package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
)

func newLogsCmd() *cobra.Command {
	var (
		unit   string
		follow bool
		tail   int32
		since  string
	)
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Stream journald entries from the node",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			c, err := newClient()
			if err != nil {
				return err
			}
			defer c.Close()

			stream, err := c.Machine.Logs(ctx, &apxv1.LogsRequest{
				Unit:      unit,
				Follow:    follow,
				TailLines: tail,
				Since:     since,
			})
			if err != nil {
				return fmt.Errorf("logs: %w", err)
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
					return fmt.Errorf("logs: %w", err)
				}
				if _, err := os.Stdout.Write(resp.Data); err != nil {
					return err
				}
			}
		},
	}
	cmd.Flags().StringVarP(&unit, "unit", "u", "", "only show entries from this unit")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow new log entries")
	cmd.Flags().Int32Var(&tail, "tail", 0, "number of lines to show (default 100)")
	cmd.Flags().StringVar(&since, "since", "", "show entries newer than the specified date/relative time")
	return cmd
}
