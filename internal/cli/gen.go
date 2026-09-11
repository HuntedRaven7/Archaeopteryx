package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/HuntedRaven7/Archaeopteryx/internal/config"
)

func newGenCmd() *cobra.Command {
	gen := &cobra.Command{
		Use:   "gen",
		Short: "Generate machine configuration and client identities offline",
		Args:  cobra.NoArgs,
	}
	gen.AddCommand(newGenConfigCmd())
	return gen
}

func newGenConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config <cluster> <endpoint>",
		Short: "Generate the machine config and client identity for a node",
		Long: `Generate, fully offline, a fresh node CA plus server and admin
identities for a single microraptor node.

  apxctl gen config factory node1 --hostname 10.0.0.5

Writes into --output-dir (default ./apx):
  apxconfig.yaml   machine config to apply to the node
  apxctl.yaml      client bundle for this CLI ("--bundle")
  ca.crt           node CA
  admin.crt/key    admin client identity`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cluster := args[0]
			endpoint := args[1]
			if outDir == "" {
				outDir = filepath.Join(".", "apx")
			}

			gen, err := config.GenerateCluster(cluster, endpoint, hostname)
			if err != nil {
				return err
			}
			written, err := gen.WriteArtifacts(outDir)
			if err != nil {
				return err
			}
			for _, p := range written {
				fmt.Printf("wrote %s\n", p)
			}
			fmt.Printf("\nApply the node config in maintenance mode from a node admin shell:\n")
			fmt.Printf("  apxctl apply-config %s --maintenance-token <token>\n", filepath.Join(outDir, "apxconfig.yaml"))
			fmt.Printf("\nThen connect with the generated bundle:\n")
			fmt.Printf("  apxctl --bundle %s status\n", filepath.Join(outDir, "apxctl.yaml"))
			return nil
		},
	}
	cmd.Flags().StringVar(&outDir, "output-dir", "", "output directory (default ./apx)")
	cmd.Flags().StringVar(&hostname, "hostname", "", "hostname of the node")
	return cmd
}

var outDir string
var hostname string
