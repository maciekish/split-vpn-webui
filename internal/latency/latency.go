package latency

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var timePattern = regexp.MustCompile(`time=([0-9]+\.?[0-9]*)`)

// Result holds the most recent latency measurement for a target.
type Result struct {
	Name          string    `json:"name"`
	Target        string    `json:"target"`
	LatencyMS     float64   `json:"latencyMs"`
	Success       bool      `json:"success"`
	CheckedAt     time.Time `json:"checkedAt"`
	Error         string    `json:"error,omitempty"`
	EverSucceeded bool      `json:"everSucceeded"`
	LastSuccess   time.Time `json:"lastSuccess,omitempty"`
}

// Monitor pings configured targets while at least one watcher is active.
type Target struct {
	Interface string
	Address   string
}

type Monitor struct {
	mu       sync.RWMutex
	interval time.Duration
	targets  map[string]Target
	results  map[string]Result
	watchers int
	stop     chan struct{}
}

// NewMonitor creates a latency monitor.
func NewMonitor(interval time.Duration) *Monitor {
	return &Monitor{
		interval: interval,
		targets:  make(map[string]Target),
		results:  make(map[string]Result),
	}
}

// UpdateTargets replaces the targets map.
func (m *Monitor) UpdateTargets(targets map[string]Target) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.targets = make(map[string]Target, len(targets))
	for name, target := range targets {
		if strings.TrimSpace(target.Address) == "" {
			continue
		}
		m.targets[name] = target
	}
}

// Activate increments the watcher count and starts the monitor if necessary.
// It returns a function that must be called to release the watcher.
func (m *Monitor) Activate() func() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.watchers++
	if m.watchers == 1 {
		m.start()
	}
	released := false
	return func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if released {
			return
		}
		released = true
		if m.watchers > 0 {
			m.watchers--
			if m.watchers == 0 {
				m.stopLoop()
			}
		}
	}
}

func (m *Monitor) start() {
	if m.stop != nil {
		return
	}
	m.stop = make(chan struct{})
	go m.loop(m.stop)
}

func (m *Monitor) stopLoop() {
	if m.stop != nil {
		close(m.stop)
		m.stop = nil
	}
}

func (m *Monitor) loop(stop <-chan struct{}) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	m.runOnce()
	for {
		select {
		case <-ticker.C:
			m.runOnce()
		case <-stop:
			return
		}
	}
}

func (m *Monitor) runOnce() {
	targets := m.snapshotTargets()
	for name, target := range targets {
		res := pingTarget(name, target)
		if res.Success {
			res.EverSucceeded = true
			res.LastSuccess = res.CheckedAt
		} else if prev, ok := m.results[name]; ok {
			res.EverSucceeded = prev.EverSucceeded || prev.Success
			if !prev.LastSuccess.IsZero() {
				res.LastSuccess = prev.LastSuccess
			} else if prev.Success {
				res.LastSuccess = prev.CheckedAt
			}
		}
		m.mu.Lock()
		m.results[name] = res
		m.mu.Unlock()
	}
}

func (m *Monitor) snapshotTargets() map[string]Target {
	m.mu.RLock()
	defer m.mu.RUnlock()
	clone := make(map[string]Target, len(m.targets))
	for k, v := range m.targets {
		clone[k] = v
	}
	return clone
}

// Results returns the latest latency results sorted by name.
func (m *Monitor) Results() []Result {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.results))
	for name := range m.results {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]Result, 0, len(names))
	for _, name := range names {
		res := m.results[name]
		out = append(out, res)
	}
	return out
}

func pingTarget(name string, target Target) Result {
	trimmedTarget := strings.TrimSpace(target.Address)
	if trimmedTarget == "" {
		return Result{Name: name, Target: target.Address, Success: false, CheckedAt: time.Now(), Error: "no target"}
	}
	args := []string{}
	iface := strings.TrimSpace(target.Interface)
	if iface != "" {
		args = append(args, "-I", iface)
	}
	args = append(args, "-c", "1", "-W", "2", trimmedTarget)
	cmd := exec.Command("ping", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	now := time.Now()
	if err != nil {
		return Result{
			Name:      name,
			Target:    trimmedTarget,
			Success:   false,
			CheckedAt: now,
			Error:     sanitizeError(err, stderr.String()),
		}
	}
	latency, parseErr := parseLatency(stdout.Bytes())
	res := Result{
		Name:      name,
		Target:    trimmedTarget,
		Success:   parseErr == nil,
		LatencyMS: latency,
		CheckedAt: now,
	}
	if parseErr != nil {
		res.Error = parseErr.Error()
	}
	return res
}

func sanitizeError(err error, stderr string) string {
	if stderr != "" {
		scanner := bufio.NewScanner(strings.NewReader(stderr))
		lines := make([]string, 0, 3)
		for scanner.Scan() {
			text := strings.TrimSpace(scanner.Text())
			if text != "" {
				lines = append(lines, text)
			}
			if len(lines) == 3 {
				break
			}
		}
		if len(lines) > 0 {
			return strings.Join(lines, "; ")
		}
	}
	return err.Error()
}

func parseLatency(output []byte) (float64, error) {
	matches := timePattern.FindSubmatch(output)
	if len(matches) != 2 {
		return 0, errors.New("latency not found")
	}
	value := string(matches[1])
	var latency float64
	if _, err := fmt.Sscanf(value, "%f", &latency); err != nil {
		return 0, err
	}
	return latency, nil
}
