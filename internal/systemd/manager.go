package systemd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

var unitNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+\.service$`)

// CommandRunner abstracts process execution for testability.
type CommandRunner interface {
	Run(name string, args ...string) error
	Output(name string, args ...string) ([]byte, error)
}

type execRunner struct{}

func (execRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

func (execRunner) Output(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

// ServiceManager defines systemd operations needed by other packages.
type ServiceManager interface {
	WriteUnit(unitName, content string) error
	RemoveUnit(unitName string) error
	Start(unitName string) error
	Stop(unitName string) error
	Restart(unitName string) error
	Enable(unitName string) error
	Disable(unitName string) error
	Status(unitName string) (string, error)
	WriteBootHook() error
}

// Manager manages systemd unit files and service operations.
type Manager struct {
	dataDir      string
	unitsDir     string
	systemdDir   string
	bootHookPath string
	runner       CommandRunner
}

// NewManager creates a manager using default UniFi paths.
func NewManager(dataDir string) *Manager {
	trimmed := strings.TrimSpace(dataDir)
	if trimmed == "" {
		trimmed = "/data/split-vpn-webui"
	}
	return &Manager{
		dataDir:      trimmed,
		unitsDir:     filepath.Join(trimmed, "units"),
		systemdDir:   "/etc/systemd/system",
		bootHookPath: "/data/on_boot.d/10-split-vpn-webui.sh",
		runner:       execRunner{},
	}
}

// NewManagerWithDeps creates a manager with custom paths and command runner.
func NewManagerWithDeps(dataDir, unitsDir, systemdDir, bootHookPath string, runner CommandRunner) *Manager {
	if runner == nil {
		runner = execRunner{}
	}
	return &Manager{
		dataDir:      dataDir,
		unitsDir:     unitsDir,
		systemdDir:   systemdDir,
		bootHookPath: bootHookPath,
		runner:       runner,
	}
}

// WriteUnit writes canonical unit content, ensures symlink in /etc/systemd/system, and reloads daemon.
func (m *Manager) WriteUnit(unitName, content string) error {
	resolved, err := normalizeUnitName(unitName)
	if err != nil {
		return err
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("unit content must not be empty")
	}
	if err := os.MkdirAll(m.unitsDir, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(m.systemdDir, 0o755); err != nil {
		return err
	}

	canonicalPath := filepath.Join(m.unitsDir, resolved)
	if err := writeFileAtomic(canonicalPath, []byte(content), 0o644); err != nil {
		return err
	}
	if err := ensureSymlink(canonicalPath, filepath.Join(m.systemdDir, resolved)); err != nil {
		return err
	}
	return m.daemonReload()
}

// RemoveUnit removes canonical unit and symlink, then reloads daemon.
func (m *Manager) RemoveUnit(unitName string) error {
	resolved, err := normalizeUnitName(unitName)
	if err != nil {
		return err
	}
	canonicalPath := filepath.Join(m.unitsDir, resolved)
	symlinkPath := filepath.Join(m.systemdDir, resolved)

	if err := os.Remove(canonicalPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Remove(symlinkPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return m.daemonReload()
}

// Start runs `systemctl start <unit>`.
func (m *Manager) Start(unitName string) error {
	return m.runSystemctl("start", unitName)
}

// Stop runs `systemctl stop <unit>`.
func (m *Manager) Stop(unitName string) error {
	return m.runSystemctl("stop", unitName)
}

// Restart runs `systemctl restart <unit>`.
func (m *Manager) Restart(unitName string) error {
	return m.runSystemctl("restart", unitName)
}

// Enable runs `systemctl enable <unit>`.
func (m *Manager) Enable(unitName string) error {
	return m.runSystemctl("enable", unitName)
}

// Disable runs `systemctl disable <unit>`.
func (m *Manager) Disable(unitName string) error {
	return m.runSystemctl("disable", unitName)
}

// Status runs `systemctl is-active <unit>` and returns the resulting state string.
func (m *Manager) Status(unitName string) (string, error) {
	resolved, err := normalizeUnitName(unitName)
	if err != nil {
		return "", err
	}
	out, runErr := m.runner.Output("systemctl", "is-active", resolved)
	status := strings.TrimSpace(string(out))
	if runErr != nil {
		return status, fmt.Errorf("systemctl is-active %s: %w", resolved, runErr)
	}
	return status, nil
}

func (m *Manager) runSystemctl(action, unitName string) error {
	resolved, err := normalizeUnitName(unitName)
	if err != nil {
		return err
	}
	if err := m.runner.Run("systemctl", action, resolved); err != nil {
		return fmt.Errorf("systemctl %s %s: %w", action, resolved, err)
	}
	return nil
}

func (m *Manager) daemonReload() error {
	if err := m.runner.Run("systemctl", "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %w", err)
	}
	return nil
}

func normalizeUnitName(unitName string) (string, error) {
	trimmed := strings.TrimSpace(unitName)
	if trimmed == "" {
		return "", fmt.Errorf("unit name is required")
	}
	if !strings.HasSuffix(trimmed, ".service") {
		trimmed += ".service"
	}
	if filepath.Base(trimmed) != trimmed || strings.ContainsAny(trimmed, `/\\`) {
		return "", fmt.Errorf("invalid unit name %q", unitName)
	}
	if !unitNamePattern.MatchString(trimmed) {
		return "", fmt.Errorf("invalid unit name %q", unitName)
	}
	return trimmed, nil
}

func writeFileAtomic(path string, content []byte, mode os.FileMode) error {
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, content, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}
	return nil
}

func ensureSymlink(targetPath, linkPath string) error {
	if existingTarget, err := os.Readlink(linkPath); err == nil {
		if existingTarget == targetPath {
			return nil
		}
		if err := os.Remove(linkPath); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		if err := os.Remove(linkPath); err != nil {
			return err
		}
	}
	return os.Symlink(targetPath, linkPath)
}
