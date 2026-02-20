package server

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"split-vpn-webui/internal/auth"
	"split-vpn-webui/internal/config"
	"split-vpn-webui/internal/latency"
	"split-vpn-webui/internal/settings"
	"split-vpn-webui/internal/stats"
	"split-vpn-webui/ui"
)

// ConfigStatus summarises runtime information for a VPN configuration.
type ConfigStatus struct {
	Name          string `json:"name"`
	Path          string `json:"path"`
	InterfaceName string `json:"interfaceName"`
	VPNType       string `json:"vpnType"`
	Gateway       string `json:"gateway"`
	Autostart     bool   `json:"autostart"`
	Connected     bool   `json:"connected"`
	OperState     string `json:"operState"`
}

// UpdatePayload is pushed to SSE listeners.
type UpdatePayload struct {
	Stats   stats.Snapshot    `json:"stats"`
	Latency []latency.Result  `json:"latency"`
	Configs []ConfigStatus    `json:"configs"`
	Errors  map[string]string `json:"errors"`
}

// Server handles HTTP requests and background coordination.
type Server struct {
	configManager *config.Manager
	stats         *stats.Collector
	latency       *latency.Monitor
	settings      *settings.Manager
	auth          *auth.Manager
	templates     *template.Template

	systemdManaged bool

	watchersMu sync.Mutex
	watchers   map[chan []byte]struct{}

	broadcastInterval time.Duration
	gatewayMu         sync.RWMutex
	gateways          map[string]string
}

// New creates an HTTP server.
func New(
	cfgManager *config.Manager,
	statsCollector *stats.Collector,
	latencyMonitor *latency.Monitor,
	settingsManager *settings.Manager,
	authManager *auth.Manager,
	systemdManaged bool,
) (*Server, error) {
	tmpl, err := template.ParseFS(ui.Assets, "web/templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}
	return &Server{
		configManager:     cfgManager,
		stats:             statsCollector,
		latency:           latencyMonitor,
		settings:          settingsManager,
		auth:              authManager,
		templates:         tmpl,
		systemdManaged:    systemdManaged,
		watchers:          make(map[chan []byte]struct{}),
		broadcastInterval: 2 * time.Second,
		gateways:          make(map[string]string),
	}, nil
}

// Router constructs the http.Handler with all routes.
func (s *Server) Router() (http.Handler, error) {
	if err := s.refreshState(); err != nil {
		return nil, err
	}
	s.applyAutostart()

	r := chi.NewRouter()
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Static assets — public, needed by the login page.
	staticFS, err := fs.Sub(ui.Assets, "web/static")
	if err != nil {
		return nil, err
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Auth endpoints — always public.
	r.Get("/login", s.handleLoginGet)
	r.Post("/login", s.handleLoginPost)
	r.Post("/logout", s.handleLogout)

	// All remaining routes require authentication.
	r.Group(func(protected chi.Router) {
		protected.Use(s.auth.Middleware)

		protected.Get("/", s.handleIndex)

		protected.Route("/api", func(api chi.Router) {
			api.Get("/configs", s.handleListConfigs)
			api.Get("/configs/{name}/file", s.handleReadConfig)
			api.Put("/configs/{name}/file", s.handleWriteConfig)
			api.Post("/configs/{name}/start", s.handleStartVPN)
			api.Post("/configs/{name}/stop", s.handleStopVPN)
			api.Post("/configs/{name}/autostart", s.handleAutostart)
			api.Post("/reload", s.handleReload)
			api.Get("/stats", s.handleStats)
			api.Get("/stream", s.handleStream)
			api.Get("/settings", s.handleGetSettings)
			api.Put("/settings", s.handleSaveSettings)
		})
	})

	return r, nil
}

// StartBackground launches the broadcaster loop.
func (s *Server) StartBackground(stop <-chan struct{}) {
	ticker := time.NewTicker(s.broadcastInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.broadcastUpdate(nil)
		case <-stop:
			return
		}
	}
}
