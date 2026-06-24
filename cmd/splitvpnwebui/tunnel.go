package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"split-vpn-webui/internal/awg"
	"split-vpn-webui/internal/vpn"
)

// runTunnelCommand implements `split-vpn-webui tunnel run --name X --data-dir D`.
// systemd starts it as a dedicated process per AmneziaWG tunnel; it blocks
// until the tunnel is stopped (SIGTERM) or dies.
func runTunnelCommand(args []string) {
	if len(args) < 1 || args[0] != "run" {
		fmt.Fprintln(os.Stderr, "usage: split-vpn-webui tunnel run --name <vpn> [--data-dir <dir>]")
		os.Exit(2)
	}

	flags := flag.NewFlagSet("tunnel run", flag.ExitOnError)
	name := flags.String("name", "", "vpn profile name")
	dataDir := flags.String("data-dir", defaultDataDir, "persistent data directory")
	if err := flags.Parse(args[1:]); err != nil {
		os.Exit(2)
	}
	if *name == "" {
		fmt.Fprintln(os.Stderr, "tunnel run: --name is required")
		os.Exit(2)
	}

	logger := log.New(os.Stderr, fmt.Sprintf("tunnel[%s] ", *name), log.LstdFlags)

	manager, err := vpn.NewManager(filepath.Join(*dataDir, "vpns"), nil, nil)
	if err != nil {
		logger.Fatalf("failed to open vpn store: %v", err)
	}
	profile, err := manager.Get(*name)
	if err != nil {
		logger.Fatalf("failed to load profile: %v", err)
	}
	spec, err := awg.BuildSpec(profile)
	if err != nil {
		logger.Fatalf("invalid profile: %v", err)
	}

	deps := &awg.BackendDeps{
		Runner: &awg.ExecRunner{Logf: logger.Printf},
		Logf:   logger.Printf,
	}
	supervisor := &awg.Supervisor{Spec: spec, Deps: deps}
	if err := supervisor.Run(context.Background()); err != nil {
		logger.Fatalf("%v", err)
	}
}
