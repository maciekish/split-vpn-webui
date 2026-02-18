package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"split-vpn-webui/internal/auth"
	"split-vpn-webui/internal/config"
	"split-vpn-webui/internal/latency"
	"split-vpn-webui/internal/settings"
	"split-vpn-webui/internal/stats"
	"split-vpn-webui/internal/util"
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

// ---- Auth handlers --------------------------------------------------------

func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	// Already authenticated — redirect to dashboard.
	if cookie, err := r.Cookie("svpn_session"); err == nil && s.auth.ValidateToken(cookie.Value) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "login.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	password := r.FormValue("password")
	if !s.auth.CheckPassword(password) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := s.templates.ExecuteTemplate(w, "login.html", map[string]any{
			"Error": "Invalid password. Please try again.",
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	token, err := s.auth.GetToken()
	if err != nil || token == "" {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "svpn_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   30 * 24 * 60 * 60, // 30 days
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     "svpn_session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ---- Page handler ---------------------------------------------------------

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, "layout.html", nil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// ---- API handlers ---------------------------------------------------------

func (s *Server) handleListConfigs(w http.ResponseWriter, r *http.Request) {
	configs, statuses, errMap := s.collectConfigStatuses()
	writeJSON(w, http.StatusOK, map[string]any{
		"configs":  configs,
		"statuses": statuses,
		"errors":   errMap,
	})
}

func (s *Server) handleReadConfig(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	content, err := s.configManager.ReadConfigFile(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": content})
}

func (s *Server) handleWriteConfig(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var payload struct {
		Content string `json:"content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if payload.Content == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "content must not be empty"})
		return
	}
	if err := s.configManager.WriteConfigFile(name, payload.Content); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleStartVPN(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cfg, err := s.configManager.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	go s.startVPN(cfg)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "starting"})
}

func (s *Server) handleStopVPN(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	cfg, err := s.configManager.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	go s.stopVPN(cfg)
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "stopping"})
}

func (s *Server) handleAutostart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var payload struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	if err := s.configManager.SetAutostart(name, payload.Enabled); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	if err := s.refreshState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
}

func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	payload := s.createPayload(nil)
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	current, err := s.settings.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	interfaces, err := util.InterfacesWithAddrs()
	if err != nil {
		interfaces = nil
	}
	// Scrub auth fields — never expose hash or token via settings API.
	safe := settings.Settings{
		ListenInterface: current.ListenInterface,
		WANInterface:    current.WANInterface,
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"settings":   safe,
		"interfaces": interfaces,
	})
}

func (s *Server) handleSaveSettings(w http.ResponseWriter, r *http.Request) {
	// Decode only the public, user-editable fields.
	var payload struct {
		ListenInterface string `json:"listenInterface"`
		WANInterface    string `json:"wanInterface"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}

	current, err := s.settings.Get()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Preserve auth fields when saving; only update network fields.
	updated := current
	updated.ListenInterface = payload.ListenInterface
	updated.WANInterface = payload.WANInterface

	if err := s.settings.Save(updated); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.refreshState(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.broadcastUpdate(nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})

	changed := current.ListenInterface != updated.ListenInterface ||
		current.WANInterface != updated.WANInterface
	if s.systemdManaged && changed {
		s.scheduleRestart()
	}
}

func (s *Server) scheduleRestart() {
	go func() {
		time.Sleep(500 * time.Millisecond)
		cmd := exec.Command("systemctl", "restart", "split-vpn-webui.service")
		if err := cmd.Run(); err != nil {
			log.Printf("systemd restart failed: %v", err)
			return
		}
		log.Printf("requested systemd restart after settings update")
	}()
}

// ---- SSE stream -----------------------------------------------------------

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering

	ch := make(chan []byte, 16)
	s.addWatcher(ch)
	defer s.removeWatcher(ch)

	release := s.latency.Activate()
	defer release()

	ctx := r.Context()
	fmt.Fprintf(w, "retry: 5000\n\n")
	flusher.Flush()

	initial := s.createPayload(nil)
	bytes, _ := json.Marshal(initial)
	fmt.Fprintf(w, "data: %s\n\n", bytes)
	flusher.Flush()

	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if len(msg) == 0 {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}

// ---- Watcher management --------------------------------------------------

func (s *Server) addWatcher(ch chan []byte) {
	s.watchersMu.Lock()
	defer s.watchersMu.Unlock()
	s.watchers[ch] = struct{}{}
}

func (s *Server) removeWatcher(ch chan []byte) {
	s.watchersMu.Lock()
	defer s.watchersMu.Unlock()
	if _, ok := s.watchers[ch]; ok {
		delete(s.watchers, ch)
		close(ch)
	}
}

func (s *Server) broadcastUpdate(errMap map[string]string) {
	s.watchersMu.Lock()
	watchers := make([]chan []byte, 0, len(s.watchers))
	for ch := range s.watchers {
		watchers = append(watchers, ch)
	}
	s.watchersMu.Unlock()
	if len(watchers) == 0 {
		return
	}
	payload := s.createPayload(errMap)
	bytes, err := json.Marshal(payload)
	if err != nil {
		return
	}
	for _, ch := range watchers {
		select {
		case ch <- bytes:
		default:
		}
	}
}

func (s *Server) createPayload(errMap map[string]string) UpdatePayload {
	snapshot := s.stats.Snapshot()
	results := s.latency.Results()
	_, statuses, configErrors := s.collectConfigStatuses()
	if errMap == nil {
		errMap = make(map[string]string)
	}
	for k, v := range configErrors {
		errMap[k] = v
	}
	return UpdatePayload{
		Stats:   snapshot,
		Latency: results,
		Configs: statuses,
		Errors:  errMap,
	}
}

// ---- State management ----------------------------------------------------

func (s *Server) refreshState() error {
	if _, err := s.configManager.Discover(); err != nil {
		// Non-fatal: directory may not exist yet on first boot.
		log.Printf("config discovery warning: %v", err)
	}
	configs, err := s.configManager.List()
	if err != nil {
		return err
	}
	vpnInterfaces := make(map[string]string)
	latencyTargets := make(map[string]latency.Target)
	resolvedGateways := make(map[string]string)
	wanCandidates := make(map[string]int)

	for _, cfg := range configs {
		if cfg.InterfaceName != "" {
			vpnInterfaces[cfg.Name] = cfg.InterfaceName
		}
		resolved := s.resolveGateway(cfg)
		resolvedGateways[cfg.Name] = resolved
		if resolved != "" {
			latencyTargets[cfg.Name] = latency.Target{
				Interface: cfg.InterfaceName,
				Address:   resolved,
			}
		}
		if wan := cfg.RawValues["WAN_INTERFACE"]; wan != "" {
			wanCandidates[wan]++
		}
	}

	s.gatewayMu.Lock()
	s.gateways = resolvedGateways
	s.gatewayMu.Unlock()

	s.latency.UpdateTargets(latencyTargets)

	storedSettings, err := s.settings.Get()
	if err != nil {
		storedSettings = settings.Settings{}
	}

	wan := storedSettings.WANInterface
	if wan == "" {
		wan = s.statsWAN()
	}
	if wan == "" {
		wan = dominantKey(wanCandidates)
	}
	if wan == "" {
		if detected, err := util.DetectWANInterface(); err == nil {
			wan = detected
		}
	}

	s.stats.ConfigureInterfaces(wan, vpnInterfaces)
	if storedSettings.WANInterface == "" {
		s.stats.SetWANInterface(wan)
	}
	return nil
}

func (s *Server) resolveGateway(cfg *config.VPNConfig) string {
	if cfg == nil {
		return ""
	}
	if gateway := strings.TrimSpace(cfg.Gateway); gateway != "" {
		return gateway
	}
	if cfg.InterfaceName == "" {
		return ""
	}
	gateway, err := util.DetectInterfaceGateway(cfg.InterfaceName)
	if err != nil {
		return ""
	}
	return gateway
}

func (s *Server) statsWAN() string {
	snap := s.stats.Snapshot()
	for _, iface := range snap.Interfaces {
		if iface.Type == stats.InterfaceWAN {
			return iface.Interface
		}
	}
	return ""
}

func (s *Server) collectConfigStatuses() ([]*config.VPNConfig, []ConfigStatus, map[string]string) {
	configs, err := s.configManager.List()
	if err != nil {
		return nil, nil, map[string]string{"configs": err.Error()}
	}
	autostart, err := s.configManager.AllAutostart()
	errMap := map[string]string{}
	if err != nil {
		errMap["autostart"] = err.Error()
	}
	s.gatewayMu.RLock()
	gatewayCopy := make(map[string]string, len(s.gateways))
	for name, value := range s.gateways {
		gatewayCopy[name] = value
	}
	s.gatewayMu.RUnlock()
	statuses := make([]ConfigStatus, 0, len(configs))
	for _, cfg := range configs {
		enabled := autostart[cfg.Name]
		connected, state := interfaceState(cfg.InterfaceName)
		gateway := gatewayCopy[cfg.Name]
		if gateway == "" {
			gateway = cfg.Gateway
		}
		statuses = append(statuses, ConfigStatus{
			Name:          cfg.Name,
			Path:          cfg.Path,
			InterfaceName: cfg.InterfaceName,
			VPNType:       cfg.VPNType,
			Gateway:       gateway,
			Autostart:     enabled,
			Connected:     connected,
			OperState:     state,
		})
	}
	sort.Slice(statuses, func(i, j int) bool { return statuses[i].Name < statuses[j].Name })
	return configs, statuses, errMap
}

// ---- VPN lifecycle -------------------------------------------------------

func (s *Server) startVPN(cfg *config.VPNConfig) {
	if err := runStartStopCommand(cfg, true); err != nil {
		s.broadcastUpdate(map[string]string{cfg.Name: err.Error()})
	} else {
		time.Sleep(2 * time.Second)
		s.broadcastUpdate(nil)
	}
}

func (s *Server) stopVPN(cfg *config.VPNConfig) {
	if err := runStartStopCommand(cfg, false); err != nil {
		s.broadcastUpdate(map[string]string{cfg.Name: err.Error()})
	} else {
		time.Sleep(1 * time.Second)
		s.broadcastUpdate(nil)
	}
}

func (s *Server) applyAutostart() {
	configs, err := s.configManager.List()
	if err != nil {
		return
	}
	for _, cfg := range configs {
		enabled, err := s.configManager.AutostartEnabled(cfg.Name)
		if err != nil || !enabled {
			continue
		}
		connected, _ := interfaceState(cfg.InterfaceName)
		if !connected {
			go s.startVPN(cfg)
		}
	}
}

func runStartStopCommand(cfg *config.VPNConfig, start bool) error {
	dir := cfg.Path
	if start {
		if script := filepath.Join(dir, "run-vpn.sh"); fileExists(script) {
			return runCommand(dir, script)
		}
		return fmt.Errorf("no start script available for %s (will be managed via systemd in a future sprint)", cfg.Name)
	}
	if script := filepath.Join(dir, "stop-vpn.sh"); fileExists(script) {
		return runCommand(dir, script)
	}
	if cfg.VPNType == "wireguard" && cfg.InterfaceName != "" {
		return runCommand("", "wg-quick", "down", cfg.InterfaceName)
	}
	if cfg.VPNType == "openvpn" {
		if cfg.InterfaceName != "" {
			// pkill with fixed args — no shell interpolation.
			return runCommand("", "pkill", "-f", "openvpn.*"+cfg.InterfaceName)
		}
		return runCommand("", "pkill", "openvpn")
	}
	return fmt.Errorf("no stop command available for %s", cfg.Name)
}

func runCommand(dir string, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// ---- Helpers -------------------------------------------------------------

func interfaceState(iface string) (bool, string) {
	if iface == "" {
		return false, ""
	}
	base := filepath.Join("/sys/class/net", iface)
	if _, err := os.Stat(base); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, "missing"
		}
		return false, "error"
	}
	data, err := os.ReadFile(filepath.Join(base, "operstate"))
	if err != nil {
		return true, "unknown"
	}
	state := strings.TrimSpace(string(data))
	return state == "up", state
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func dominantKey(counts map[string]int) string {
	highest := 0
	winner := ""
	for key, count := range counts {
		if count > highest {
			highest = count
			winner = key
		}
	}
	return winner
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(data)
}
