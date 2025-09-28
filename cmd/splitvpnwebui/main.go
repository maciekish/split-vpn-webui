package main

import (
	"context"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"split-vpn-webui/internal/config"
	"split-vpn-webui/internal/latency"
	"split-vpn-webui/internal/server"
	"split-vpn-webui/internal/settings"
	"split-vpn-webui/internal/stats"
	"split-vpn-webui/internal/util"
)

func main() {
	addr := flag.String("addr", ":8091", "listen address")
	baseDir := flag.String("split-vpn-dir", "/mnt/data/split-vpn", "path to split-vpn directory")
	poll := flag.Duration("poll", 2*time.Second, "statistics poll interval")
	history := flag.Int("history", 120, "number of samples to retain for charts")
	latencyInterval := flag.Duration("latency-interval", 10*time.Second, "latency refresh interval")
	systemdMode := flag.Bool("systemd", false, "indicate the process is managed by systemd")
	flag.Parse()

	cfgManager := config.NewManager(*baseDir)
	settingsManager := settings.NewManager(*baseDir)

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

	srv, err := server.New(cfgManager, collector, latencyMonitor, settingsManager, *systemdMode)
	if err != nil {
		log.Fatalf("failed to build server: %v", err)
	}

	router, err := srv.Router()
	if err != nil {
		log.Fatalf("failed to prepare router: %v", err)
	}

	stop := make(chan struct{})
	go collector.Start(stop)
	go srv.StartBackground(stop)

	httpServer := &http.Server{
		Addr:         listenAddr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("split-vpn web ui listening on %s", listenAddr)
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
