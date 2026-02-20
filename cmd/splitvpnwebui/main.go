package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"split-vpn-webui/internal/auth"
	"split-vpn-webui/internal/config"
	"split-vpn-webui/internal/database"
	"split-vpn-webui/internal/latency"
	"split-vpn-webui/internal/prewarm"
	"split-vpn-webui/internal/routing"
	"split-vpn-webui/internal/server"
	"split-vpn-webui/internal/settings"
	"split-vpn-webui/internal/stats"
	"split-vpn-webui/internal/systemd"
	"split-vpn-webui/internal/util"
	"split-vpn-webui/internal/vpn"
)

const defaultDataDir = "/data/split-vpn-webui"

func main() {
	addr := flag.String("addr", ":8091", "listen address (host:port)")
	dataDir := flag.String("data-dir", defaultDataDir, "persistent data directory")
	dbPath := flag.String("db", "", "SQLite database path (defaults to <data-dir>/stats.db)")
	poll := flag.Duration("poll", 2*time.Second, "statistics poll interval")
	history := flag.Int("history", 120, "number of samples to retain for charts")
	latencyInterval := flag.Duration("latency-interval", 10*time.Second, "latency refresh interval")
	systemdMode := flag.Bool("systemd", false, "indicate the process is managed by systemd")
	flag.Parse()

	// Ensure the data directory tree exists.
	for _, sub := range []string{"", "vpns", "units", "logs"} {
		dir := filepath.Join(*dataDir, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("failed to create directory %s: %v", dir, err)
		}
	}

	resolvedDB := *dbPath
	if resolvedDB == "" {
		resolvedDB = filepath.Join(*dataDir, "stats.db")
	}

	db, err := database.Open(resolvedDB)
	if err != nil {
		log.Fatalf("failed to open database %s: %v", resolvedDB, err)
	}
	defer db.Close()

	settingsPath := filepath.Join(*dataDir, "settings.json")
	settingsManager := settings.NewManager(settingsPath)

	authManager := auth.NewManager(settingsManager)
	if err := authManager.EnsureDefaults(); err != nil {
		log.Fatalf("failed to initialise auth: %v", err)
	}

	// VPN config discovery scans the vpns/ subdirectory.
	vpnsDir := filepath.Join(*dataDir, "vpns")
	cfgManager := config.NewManager(vpnsDir)
	systemdManager := systemd.NewManager(*dataDir)
	if err := systemdManager.WriteBootHook(); err != nil {
		log.Printf("warning: failed to write boot hook: %v", err)
	}
	vpnManager, err := vpn.NewManager(vpnsDir, nil, systemdManager)
	if err != nil {
		log.Fatalf("failed to initialize vpn manager: %v", err)
	}
	routingManager, err := routing.NewManager(db, vpnManager)
	if err != nil {
		log.Fatalf("failed to initialize routing manager: %v", err)
	}
	if err := routingManager.Apply(context.Background()); err != nil {
		log.Printf("warning: failed to apply routing state on startup: %v", err)
	}
	prewarmScheduler, err := prewarm.NewScheduler(db, settingsManager, routingManager, vpnManager, nil)
	if err != nil {
		log.Fatalf("failed to initialize prewarm scheduler: %v", err)
	}

	storedSettings, err := settingsManager.Get()
	if err != nil {
		log.Printf("warning: failed to load settings: %v", err)
	}

	collector := stats.NewCollector("", *poll, *history)
	if storedSettings.WANInterface != "" {
		collector.SetWANInterface(storedSettings.WANInterface)
	}
	latencyMonitor := latency.NewMonitor(*latencyInterval)

	listenAddr := resolveListenAddress(*addr, storedSettings.ListenInterface)

	srv, err := server.New(cfgManager, vpnManager, routingManager, prewarmScheduler, systemdManager, collector, latencyMonitor, settingsManager, authManager, *systemdMode)
	if err != nil {
		log.Fatalf("failed to build server: %v", err)
	}
	if err := prewarmScheduler.Start(); err != nil {
		log.Fatalf("failed to start prewarm scheduler: %v", err)
	}
	defer func() {
		if err := prewarmScheduler.Stop(); err != nil {
			log.Printf("prewarm scheduler stop warning: %v", err)
		}
	}()

	router, err := srv.Router()
	if err != nil {
		log.Fatalf("failed to prepare router: %v", err)
	}

	stop := make(chan struct{})
	go collector.Start(stop)
	go srv.StartBackground(stop)

	httpServer := &http.Server{
		Addr:        listenAddr,
		Handler:     router,
		ReadTimeout: 15 * time.Second,
		// WriteTimeout is intentionally not set (or set long) because SSE
		// connections are long-lived; a strict timeout would drop them.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("split-vpn-webui listening on %s (data: %s)", listenAddr, *dataDir)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server error: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	<-sigCh
	log.Println("shutting down...")
	close(stop)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown error: %v", err)
	}
}

func resolveListenAddress(defaultAddr, listenInterface string) string {
	host, port, err := net.SplitHostPort(defaultAddr)
	if err != nil {
		trimmed := strings.TrimPrefix(defaultAddr, ":")
		if trimmed == "" {
			port = "8091"
		} else {
			port = trimmed
		}
		host = ""
	}
	if listenInterface == "" {
		if host == "" {
			return ":" + port
		}
		return net.JoinHostPort(host, port)
	}
	ip, err := util.InterfaceIPv4(listenInterface)
	if err != nil || ip == "" {
		log.Printf("warning: unable to resolve IP for interface %s: %v", listenInterface, err)
		if host == "" {
			return ":" + port
		}
		return net.JoinHostPort(host, port)
	}
	return net.JoinHostPort(ip, port)
}
