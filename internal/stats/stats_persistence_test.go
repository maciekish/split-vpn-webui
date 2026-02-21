package stats

import (
	"testing"
	"time"

	"split-vpn-webui/internal/database"
)

func TestPersistAndLoadHistoryRoundTrip(t *testing.T) {
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	source := NewCollector("eth0", 2*time.Second, 10)
	source.ConfigureInterfaces("eth0", map[string]string{"vpn-a": "wg0"})

	t1 := time.Unix(1700000000, 0)
	t2 := t1.Add(2 * time.Second)
	source.mu.Lock()
	source.interfaces["WAN"].History = []datapoint{
		{Timestamp: t1, RxThroughput: 100, TxThroughput: 80, TotalThroughput: 180, RxBytes: 1000, TxBytes: 900},
		{Timestamp: t2, RxThroughput: 120, TxThroughput: 90, TotalThroughput: 210, RxBytes: 1100, TxBytes: 980},
	}
	source.interfaces["WAN"].Available = true
	source.interfaces["WAN"].LastUpdated = t2
	source.interfaces["WAN"].RxBytes = 1100
	source.interfaces["WAN"].TxBytes = 980
	source.interfaces["WAN"].TotalBytes = 2080
	source.interfaces["WAN"].CurrentRxThroughput = 120
	source.interfaces["WAN"].CurrentTxThroughput = 90
	source.interfaces["WAN"].CurrentThroughput = 210

	source.interfaces["vpn-a"].History = []datapoint{
		{Timestamp: t1, RxThroughput: 40, TxThroughput: 50, TotalThroughput: 90, RxBytes: 400, TxBytes: 500},
		{Timestamp: t2, RxThroughput: 45, TxThroughput: 55, TotalThroughput: 100, RxBytes: 430, TxBytes: 540},
	}
	source.interfaces["vpn-a"].Available = true
	source.interfaces["vpn-a"].LastUpdated = t2
	source.interfaces["vpn-a"].RxBytes = 430
	source.interfaces["vpn-a"].TxBytes = 540
	source.interfaces["vpn-a"].TotalBytes = 970
	source.interfaces["vpn-a"].CurrentRxThroughput = 45
	source.interfaces["vpn-a"].CurrentTxThroughput = 55
	source.interfaces["vpn-a"].CurrentThroughput = 100
	source.mu.Unlock()

	if err := source.Persist(db); err != nil {
		t.Fatalf("persist: %v", err)
	}

	restored := NewCollector("eth0", 2*time.Second, 10)
	restored.ConfigureInterfaces("eth0", map[string]string{"vpn-a": "wg0"})
	if err := restored.LoadHistory(db); err != nil {
		t.Fatalf("load history: %v", err)
	}

	restored.mu.RLock()
	defer restored.mu.RUnlock()

	wan := restored.interfaces["WAN"]
	if wan == nil {
		t.Fatalf("expected WAN interface")
	}
	if len(wan.History) != 2 {
		t.Fatalf("expected 2 WAN points, got %d", len(wan.History))
	}
	if wan.RxBytes != 1100 || wan.TxBytes != 980 {
		t.Fatalf("unexpected WAN bytes: rx=%d tx=%d", wan.RxBytes, wan.TxBytes)
	}
	if !wan.Available {
		t.Fatalf("expected WAN available after load")
	}

	vpn := restored.interfaces["vpn-a"]
	if vpn == nil {
		t.Fatalf("expected vpn-a interface")
	}
	if len(vpn.History) != 2 {
		t.Fatalf("expected 2 VPN points, got %d", len(vpn.History))
	}
	if vpn.RxBytes != 430 || vpn.TxBytes != 540 {
		t.Fatalf("unexpected VPN bytes: rx=%d tx=%d", vpn.RxBytes, vpn.TxBytes)
	}
}

func TestLoadHistoryPendingUntilInterfaceConfigured(t *testing.T) {
	db, err := database.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ts := time.Unix(1700000100, 0).Unix()
	if _, err := db.Exec(`
		INSERT INTO stats_history (interface, timestamp, rx_bytes, tx_bytes)
		VALUES ('vpn-pending', ?, 123, 456)
	`, ts); err != nil {
		t.Fatalf("seed stats_history: %v", err)
	}

	collector := NewCollector("", 2*time.Second, 10)
	if err := collector.LoadHistory(db); err != nil {
		t.Fatalf("load history: %v", err)
	}

	collector.mu.RLock()
	if len(collector.pendingHistory) != 1 {
		collector.mu.RUnlock()
		t.Fatalf("expected pending history entry, got %d", len(collector.pendingHistory))
	}
	collector.mu.RUnlock()

	collector.ConfigureInterfaces("", map[string]string{"vpn-pending": "wg9"})

	collector.mu.RLock()
	defer collector.mu.RUnlock()
	if len(collector.pendingHistory) != 0 {
		t.Fatalf("expected pending history to be applied")
	}
	iface := collector.interfaces["vpn-pending"]
	if iface == nil {
		t.Fatalf("expected vpn-pending interface")
	}
	if len(iface.History) != 1 {
		t.Fatalf("expected one restored datapoint, got %d", len(iface.History))
	}
	if iface.RxBytes != 123 || iface.TxBytes != 456 {
		t.Fatalf("unexpected restored bytes: rx=%d tx=%d", iface.RxBytes, iface.TxBytes)
	}
}
