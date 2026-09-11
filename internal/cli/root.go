// Package cli implements the apxctl command tree.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/HuntedRaven7/Archaeopteryx/pkg/client"
)

// GlobalFlags hold connection options shared by remote subcommands.
type GlobalFlags struct {
	Endpoint string
	CA       string
	Cert     string
	Key      string
	Insecure bool
}

var global GlobalFlags

// NewRootCmd builds the apxctl root command.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "apxctl",
		Short:         "Control a microraptor node over the apx control plane API",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&global.Endpoint, "endpoint", envOr("APX_ENDPOINT", "127.0.0.1:50000"), "apxd endpoint host:port")
	root.PersistentFlags().StringVar(&global.CA, "ca", envOr("APX_CA", ""), "path to node CA PEM")
	root.PersistentFlags().StringVar(&global.Cert, "cert", envOr("APX_CERT", ""), "path to client cert PEM")
	root.PersistentFlags().StringVar(&global.Key, "key", envOr("APX_KEY", ""), "path to client key PEM")
	root.PersistentFlags().BoolVar(&global.Insecure, "insecure", false, "skip TLS verification and client auth")

	root.AddCommand(
		newVersionCmd(),
		newStatusCmd(),
	)

	return root
}

// Execute runs the CLI.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "apxctl: %v\n", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// newClient builds a client from the global flags and a common deadline.
func newClient() (*client.Client, error) {
	return client.New(client.Options{
		Endpoint: global.Endpoint,
		CA:       global.CA,
		Cert:     global.Cert,
		Key:      global.Key,
		Insecure: global.Insecure,
	})
}
