package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
	"github.com/HuntedRaven7/Archaeopteryx/pkg/version"
)

var emptyVersionReq = apxv1.VersionRequest{}

func newVersionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "version",
		Short: "Print client and server version information",
		RunE: func(cmd *cobra.Command, args []string) error {
			v := version.Current()
			fmt.Printf("Client:\n")
			printVersion(v)
			fmt.Printf("Server (%s):\n", global.Endpoint)
			if global.CA == "" && global.Cert == "" && global.Key == "" {
				fmt.Printf("  <not queried, no credentials configured>\n")
				return nil
			}
			c, err := newClient()
			if err != nil {
				return err
			}
			defer c.Close()

			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			sv, err := c.Machine.Version(ctx, &emptyVersionReq)
			if err != nil {
				return fmt.Errorf("query server version: %w", err)
			}
			printVersion(version.Info{
				Version:   sv.Version,
				Commit:    sv.Commit,
				BuildDate: sv.BuildDate,
				GoVersion: sv.GoVersion,
				Platform:  sv.Platform,
			})
			return nil
		},
	}
	return cmd
}

func printVersion(v version.Info) {
	fmt.Printf("  version:   %s\n", v.Version)
	fmt.Printf("  commit:    %s\n", v.Commit)
	fmt.Printf("  built:     %s\n", v.BuildDate)
	fmt.Printf("  go:        %s\n", v.GoVersion)
	fmt.Printf("  platform:  %s\n", v.Platform)
}
