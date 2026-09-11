package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	apxv1 "github.com/HuntedRaven7/Archaeopteryx/api/apx/v1"
	"github.com/HuntedRaven7/Archaeopteryx/internal/machine"
)

type kubeconfigCmdOptions struct {
	merge bool
	file  string
}

func newKubeconfigCmd() *cobra.Command {
	var opts kubeconfigCmdOptions
	cmd := &cobra.Command{
		Use:   "kubeconfig",
		Short: "Fetch the admin kubeconfig for the k0s cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := newClient()
			if err != nil {
				return err
			}
			defer c.Close()

			ctx, cancel := context.WithTimeout(cmd.Context(), 30*time.Second)
			defer cancel()

			resp, err := c.Machine.Kubeconfig(ctx, &apxv1.KubeconfigRequest{})
			if err != nil {
				return fmt.Errorf("kubeconfig: %w", err)
			}

			dest := opts.file
			if dest == "" && opts.merge {
				dest = defaultKubeconfig()
			}
			if dest != "" {
				target, err := os.ReadFile(dest)
				if err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("read %s: %w", dest, err)
				}
				if opts.merge {
					merged, err := machine.MergeKubeconfig(target, resp.Kubeconfig, mergeName())
					if err != nil {
						return err
					}
					target = merged
				} else {
					target = resp.Kubeconfig
				}
				if err := writeKubeconfig(dest, target); err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\n", dest)
				return nil
			}
			_, err = cmd.OutOrStdout().Write(resp.Kubeconfig)
			return err
		},
	}
	cmd.Flags().BoolVar(&opts.merge, "merge", false, "merge into the existing kubeconfig (default ~/.kube/config) as an apx-<name> context")
	cmd.Flags().StringVar(&opts.file, "file", "", "write the kubeconfig to this file")
	return cmd
}

func defaultKubeconfig() string {
	if k := os.Getenv("KUBECONFIG"); k != "" {
		return k
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".kube/config"
	}
	return filepath.Join(home, ".kube", "config")
}

// mergeName derives the apx context/cluster/user name from the endpoint host.
func mergeName() string {
	host := strings.Split(stripScheme(contextEndpoint()), ":")[0]
	if host == "" {
		return "node"
	}
	return host
}

// contextEndpoint returns the endpoint the client resolves, honoring env and
// defaults so the generated context name is deterministic in scripts.
func contextEndpoint() string {
	if global.Endpoint != "" {
		return global.Endpoint
	}
	if e := os.Getenv("APX_ENDPOINT"); e != "" {
		return e
	}
	return "127.0.0.1:50000"
}

func writeKubeconfig(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".kubeconfig-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
