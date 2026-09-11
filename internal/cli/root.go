package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/HuntedRaven7/Archaeopteryx/internal/config"
	"github.com/HuntedRaven7/Archaeopteryx/pkg/client"
)

// GlobalFlags hold connection options shared by remote subcommands.
type GlobalFlags struct {
	Endpoint string
	CA       string
	Cert     string
	Key      string
	Bundle   string
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
	root.PersistentFlags().StringVar(&global.Endpoint, "endpoint", envOr("APX_ENDPOINT", ""), "apxd endpoint host:port")
	root.PersistentFlags().StringVar(&global.CA, "ca", envOr("APX_CA", ""), "path to node CA PEM")
	root.PersistentFlags().StringVar(&global.Cert, "cert", envOr("APX_CERT", ""), "path to client cert PEM")
	root.PersistentFlags().StringVar(&global.Key, "key", envOr("APX_KEY", ""), "path to client key PEM")
	root.PersistentFlags().StringVar(&global.Bundle, "bundle", envOr("APX_BUNDLE", ""), "client bundle (apxctl.yaml) providing endpoint and identity")
	root.PersistentFlags().BoolVar(&global.Insecure, "insecure", false, "skip TLS verification and client auth")

	root.AddCommand(
		newVersionCmd(),
		newStatusCmd(),
		newGenCmd(),
		newApplyConfigCmd(),
		newConfigCmd(),
		newLogsCmd(),
		newServicesCmd(),
		newServiceCmd(),
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

// resolvedOptions merges explicit flags with the bundle: bundle context is
// used to fill any identity/endpoint not already provided by flags.
func resolvedOptions() (client.Options, error) {
	endpoint := global.Endpoint
	if endpoint == "" {
		endpoint = "127.0.0.1:50000"
	}
	opts := client.Options{
		Endpoint: stripScheme(endpoint),
		CA:       global.CA,
		Cert:     global.Cert,
		Key:      global.Key,
		Insecure: global.Insecure,
	}

	if global.Bundle != "" && global.CA == "" && global.Cert == "" && global.Key == "" {
		b, err := os.ReadFile(global.Bundle)
		if err != nil {
			return client.Options{}, fmt.Errorf("read bundle: %w", err)
		}
		bd, err := config.ParseClientBundle(b)
		if err != nil {
			return client.Options{}, err
		}
		ctx := bd.First()
		if ctx == nil {
			return client.Options{}, fmt.Errorf("bundle has no contexts")
		}
		opts.Endpoint = stripScheme(ctx.Endpoint)
		opts.CA = writeTempIdentity(ctx.CA, "ca.crt")
		opts.Cert = writeTempIdentity(ctx.Cert, "client.crt")
		opts.Key = writeTempIdentity(ctx.Key, "client.key")
	}
	return opts, nil
}

// newClient builds a client from the global flags and a common deadline.
func newClient() (*client.Client, error) {
	opts, err := resolvedOptions()
	if err != nil {
		return nil, err
	}
	return client.New(opts)
}

// stripScheme normalizes an endpoint URL to host:port for grpc.
func stripScheme(endpoint string) string {
	if i := strings.Index(endpoint, "://"); i >= 0 {
		return endpoint[i+3:]
	}
	return endpoint
}

// writeTempIdentity materializes an in-bundle PEM into a temp file so the
// client layer can read a cert/key pair like a file-based setup.
func writeTempIdentity(pem, suffix string) string {
	if pem == "" {
		return ""
	}
	f, err := os.CreateTemp("", "apx-"+suffix)
	if err != nil {
		return ""
	}
	if _, err := f.WriteString(pem + "\n"); err != nil {
		return ""
	}
	f.Close()
	return f.Name()
}
