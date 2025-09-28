package stats

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type InterfaceType string

const (
	InterfaceWAN InterfaceType = "wan"
	InterfaceVPN InterfaceType = "vpn"
)

type datapoint struct {
	Timestamp       time.Time `json:"timestamp"`
	RxThroughput    float64   `json:"rxThroughput"`
	TxThroughput    float64   `json:"txThroughput"`
	TotalThroughput float64   `json:"totalThroughput"`
}

// InterfaceStats summarises live metrics for a network interface.
type InterfaceStats struct {
	Name                string        `json:"name"`
	Interface           string        `json:"interface"`
	Type                InterfaceType `json:"type"`
	RxBytes             uint64        `json:"rxBytes"`
	TxBytes             uint64        `json:"txBytes"`
	TotalBytes          uint64        `json:"totalBytes"`
	CurrentThroughput   float64       `json:"currentThroughput"`
	CurrentRxThroughput float64       `json:"currentRxThroughput"`
	CurrentTxThroughput float64       `json:"currentTxThroughput"`
	History             []datapoint   `json:"history"`
	Available           bool          `json:"available"`
	LastUpdated         time.Time     `json:"lastUpdated"`
	OperState           string        `json:"operState,omitempty"`

	baseRx uint64
	baseTx uint64
}

// Snapshot contains the latest statistics for all monitored interfaces.
type Snapshot struct {
	Interfaces             []*InterfaceStats `json:"interfaces"`
	GeneratedAt            time.Time         `json:"generatedAt"`
	WanCorrectedThroughput float64           `json:"wanCorrectedThroughput"`
	WanCorrectedBytes      uint64            `json:"wanCorrectedBytes"`
}

// Collector monitors interface statistics.
type Collector struct {
	mu            sync.RWMutex
	interfaces    map[string]*InterfaceStats
	historyLength int
	pollInterval  time.Duration
	wanInterface  string
}

// NewCollector instantiates a collector.
func NewCollector(wanInterface string, pollInterval time.Duration, historyLength int) *Collector {
	return &Collector{
		interfaces:    make(map[string]*InterfaceStats),
		historyLength: historyLength,
		pollInterval:  pollInterval,
		wanInterface:  wanInterface,
	}
}

// SetWANInterface updates the tracked WAN interface.
func (c *Collector) SetWANInterface(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.wanInterface = name
}

// ConfigureInterfaces ensures the collector is tracking the provided interface names.
func (c *Collector) ConfigureInterfaces(wan string, vpnInterfaces map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if wan != "" {
		c.wanInterface = wan
		c.ensureInterface("WAN", wan, InterfaceWAN)
	}
	for key, iface := range c.interfaces {
		if iface.Type != InterfaceVPN {
			continue
		}
		if _, ok := vpnInterfaces[key]; !ok {
			delete(c.interfaces, key)
		}
	}
	for name, ifName := range vpnInterfaces {
		c.ensureInterface(name, ifName, InterfaceVPN)
	}
}

func (c *Collector) ensureInterface(name, iface string, ifaceType InterfaceType) {
	if existing, ok := c.interfaces[name]; ok {
		if existing.Interface != iface {
			existing.Interface = iface
			existing.baseRx = 0
			existing.baseTx = 0
			existing.Available = false
			existing.LastUpdated = time.Time{}
			existing.RxBytes = 0
			existing.TxBytes = 0
			existing.TotalBytes = 0
			existing.CurrentThroughput = 0
			existing.CurrentRxThroughput = 0
			existing.CurrentTxThroughput = 0
			existing.History = existing.History[:0]
		}
		existing.Type = ifaceType
		return
	}
	c.interfaces[name] = &InterfaceStats{
		Name:      name,
		Interface: iface,
		Type:      ifaceType,
		History:   make([]datapoint, 0, c.historyLength),
	}
}

// Start begins the polling loop.
func (c *Collector) Start(stop <-chan struct{}) {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	c.update(time.Now())
	for {
		select {
		case t := <-ticker.C:
			c.update(t)
		case <-stop:
			return
		}
	}
}

func (c *Collector) update(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, stats := range c.interfaces {
		if state, err := readInterfaceState(stats.Interface); err == nil {
			stats.OperState = state
		} else {
			stats.OperState = ""
		}

		rx, tx, err := readInterfaceBytes(stats.Interface)
		if err != nil {
			stats.Available = false
			stats.CurrentThroughput = 0
			stats.CurrentRxThroughput = 0
			stats.CurrentTxThroughput = 0
			continue
		}
		if stats.baseRx == 0 && stats.baseTx == 0 && !stats.Available {
			stats.baseRx = rx
			stats.baseTx = tx
		}
		if rx < stats.baseRx {
			stats.baseRx = rx
		}
		if tx < stats.baseTx {
			stats.baseTx = tx
		}
		adjRx := rx - stats.baseRx
		adjTx := tx - stats.baseTx
		total := adjRx + adjTx
		elapsed := c.pollInterval
		if stats.Available && !stats.LastUpdated.IsZero() {
			elapsed = now.Sub(stats.LastUpdated)
			if elapsed <= 0 {
				elapsed = c.pollInterval
			}
		}
		prevRx := stats.RxBytes
		prevTx := stats.TxBytes
		deltaRxBytes := float64(0)
		deltaTxBytes := float64(0)
		if adjRx >= prevRx {
			deltaRxBytes = float64(adjRx - prevRx)
		}
		if adjTx >= prevTx {
			deltaTxBytes = float64(adjTx - prevTx)
		}
		seconds := elapsed.Seconds()
		if seconds <= 0 {
			seconds = c.pollInterval.Seconds()
		}
		stats.RxBytes = adjRx
		stats.TxBytes = adjTx
		stats.TotalBytes = total
		currentRx := (deltaRxBytes / seconds) * 8
		currentTx := (deltaTxBytes / seconds) * 8
		stats.CurrentRxThroughput = currentRx
		stats.CurrentTxThroughput = currentTx
		stats.CurrentThroughput = currentRx + currentTx
		stats.History = append(stats.History, datapoint{
			Timestamp:       now,
			RxThroughput:    currentRx,
			TxThroughput:    currentTx,
			TotalThroughput: stats.CurrentThroughput,
		})
		if len(stats.History) > c.historyLength {
			stats.History = stats.History[len(stats.History)-c.historyLength:]
		}
		stats.LastUpdated = now
		stats.Available = true
	}
}

// Snapshot returns a copy of the current statistics with WAN adjustments.
func (c *Collector) Snapshot() Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()

	copies := make([]*InterfaceStats, 0, len(c.interfaces))
	var wanStats *InterfaceStats
	var vpnTotalsRxThroughput float64
	var vpnTotalsTxThroughput float64
	var vpnTotalsRxBytes uint64
	var vpnTotalsTxBytes uint64

	for _, stats := range c.interfaces {
		clone := *stats
		clone.History = append([]datapoint(nil), stats.History...)
		copies = append(copies, &clone)
		if stats.Type == InterfaceWAN {
			wanStats = &clone
		} else if stats.Type == InterfaceVPN {
			vpnTotalsRxThroughput += stats.CurrentRxThroughput
			vpnTotalsTxThroughput += stats.CurrentTxThroughput
			vpnTotalsRxBytes += stats.RxBytes
			vpnTotalsTxBytes += stats.TxBytes
		}
	}

	snap := Snapshot{
		Interfaces:  copies,
		GeneratedAt: time.Now(),
	}

	if wanStats != nil {
		correctedRx := wanStats.CurrentRxThroughput - vpnTotalsRxThroughput
		if correctedRx < 0 {
			correctedRx = 0
		}
		correctedTx := wanStats.CurrentTxThroughput - vpnTotalsTxThroughput
		if correctedTx < 0 {
			correctedTx = 0
		}
		wanStats.CurrentRxThroughput = correctedRx
		wanStats.CurrentTxThroughput = correctedTx
		wanStats.CurrentThroughput = correctedRx + correctedTx

		correctedRxBytes := wanStats.RxBytes
		if correctedRxBytes > vpnTotalsRxBytes {
			correctedRxBytes -= vpnTotalsRxBytes
		} else {
			correctedRxBytes = 0
		}
		correctedTxBytes := wanStats.TxBytes
		if correctedTxBytes > vpnTotalsTxBytes {
			correctedTxBytes -= vpnTotalsTxBytes
		} else {
			correctedTxBytes = 0
		}
		wanStats.RxBytes = correctedRxBytes
		wanStats.TxBytes = correctedTxBytes
		wanStats.TotalBytes = correctedRxBytes + correctedTxBytes
		snap.WanCorrectedThroughput = wanStats.CurrentThroughput
		snap.WanCorrectedBytes = wanStats.TotalBytes
	}

	sort.SliceStable(copies, func(i, j int) bool {
		if copies[i].Type != copies[j].Type {
			return copies[i].Type == InterfaceWAN
		}
		return copies[i].Name < copies[j].Name
	})

	return snap
}

func readInterfaceBytes(iface string) (uint64, uint64, error) {
	if iface == "" {
		return 0, 0, errors.New("interface not specified")
	}
	base := filepath.Join("/sys/class/net", iface, "statistics")
	rx, err := readUintFromFile(filepath.Join(base, "rx_bytes"))
	if err != nil {
		return 0, 0, fmt.Errorf("rx_bytes: %w", err)
	}
	tx, err := readUintFromFile(filepath.Join(base, "tx_bytes"))
	if err != nil {
		return 0, 0, fmt.Errorf("tx_bytes: %w", err)
	}
	return rx, tx, nil
}

func readUintFromFile(path string) (uint64, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var value uint64
	_, err = fmt.Sscanf(string(bytes), "%d", &value)
	return value, err
}

func readInterfaceState(iface string) (string, error) {
	if strings.TrimSpace(iface) == "" {
		return "", errors.New("interface not specified")
	}
	path := filepath.Join("/sys/class/net", iface, "operstate")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}
