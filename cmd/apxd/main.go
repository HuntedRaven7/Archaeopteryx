// Command apxd is the node-side apx control plane daemon for microraptor.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/HuntedRaven7/Archaeopteryx/internal/apid"
	"github.com/HuntedRaven7/Archaeopteryx/internal/config"
)

func main() {
	listen := flag.String("listen", config.DefaultListen, "gRPC listen address")
	stateDir := flag.String("state-dir", config.DefaultStateDir, "state and PKI directory")
	configPath := flag.String("config", "", "machine config path (default /var/lib/archaeopteryx/config.yaml)")
	flag.Parse()

	srv, err := apid.New(apid.Config{
		Listen:     *listen,
		StateDir:   *stateDir,
		ConfigPath: *configPath,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "apxd: %v\n", err)
		os.Exit(1)
	}
	srv.Register()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	srv.LogMaintenance(log)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := srv.Serve(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "apxd: %v\n", err)
		os.Exit(1)
	}
}
