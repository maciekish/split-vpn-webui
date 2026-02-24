package server

import (
	"errors"
	"testing"
	"time"
)

func TestVPNFlowInspectorTracksDeltasAndRetention(t *testing.T) {
	base := time.Date(2026, time.February, 24, 12, 0, 0, 0, time.UTC)
	current := base
	inspector := newVPNFlowInspector()
	inspector.now = func() time.Time { return current }

	sessionID, err := inspector.startSession("wg-sgp", "wg-sv-sgp")
	if err != nil {
		t.Fatalf("startSession failed: %v", err)
	}

	first, err := inspector.updateAndSnapshot("wg-sgp", sessionID, []flowInspectorSample{
		{
			Key:             "tcp|10.0.1.10|50231|142.250.74.14|443",
			Protocol:        "tcp",
			SourceIP:        "10.0.1.10",
			SourcePort:      50231,
			DestinationIP:   "142.250.74.14",
			DestinationPort: 443,
			UploadBytes:     100,
			DownloadBytes:   200,
		},
	})
	if err != nil {
		t.Fatalf("first update failed: %v", err)
	}
	if first.FlowCount != 1 {
		t.Fatalf("expected one flow, got %d", first.FlowCount)
	}
	if first.Totals.TotalBytes != 0 {
		t.Fatalf("expected first sample totals to start at zero, got %d", first.Totals.TotalBytes)
	}

	current = current.Add(2 * time.Second)
	second, err := inspector.updateAndSnapshot("wg-sgp", sessionID, []flowInspectorSample{
		{
			Key:             "tcp|10.0.1.10|50231|142.250.74.14|443",
			Protocol:        "tcp",
			SourceIP:        "10.0.1.10",
			SourcePort:      50231,
			DestinationIP:   "142.250.74.14",
			DestinationPort: 443,
			UploadBytes:     220,
			DownloadBytes:   260,
		},
	})
	if err != nil {
		t.Fatalf("second update failed: %v", err)
	}
	if second.Totals.UploadBytes != 120 || second.Totals.DownloadBytes != 60 {
		t.Fatalf("unexpected totals: %#v", second.Totals)
	}
	if len(second.Flows) != 1 {
		t.Fatalf("expected one flow row, got %d", len(second.Flows))
	}
	row := second.Flows[0]
	if row.UploadBytes != 120 || row.DownloadBytes != 60 {
		t.Fatalf("unexpected per-flow totals: %#v", row)
	}
	if row.UploadBps != 480 || row.DownloadBps != 240 {
		t.Fatalf("unexpected throughput: upload=%f download=%f", row.UploadBps, row.DownloadBps)
	}

	current = current.Add(5 * time.Minute)
	stillRetained, err := inspector.updateAndSnapshot("wg-sgp", sessionID, nil)
	if err != nil {
		t.Fatalf("retained update failed: %v", err)
	}
	if stillRetained.FlowCount != 1 {
		t.Fatalf("expected flow to stay retained before timeout, got %d", stillRetained.FlowCount)
	}
	if stillRetained.Flows[0].UploadBps != 0 || stillRetained.Flows[0].DownloadBps != 0 {
		t.Fatalf("expected retained idle flow rates to be zero, got %#v", stillRetained.Flows[0])
	}

	current = current.Add(6 * time.Minute)
	expired, err := inspector.updateAndSnapshot("wg-sgp", sessionID, nil)
	if err != nil {
		t.Fatalf("expired update failed: %v", err)
	}
	if expired.FlowCount != 0 {
		t.Fatalf("expected idle flow to be evicted after timeout, got %d", expired.FlowCount)
	}
}

func TestVPNFlowInspectorSessionMismatch(t *testing.T) {
	inspector := newVPNFlowInspector()
	sessionID, err := inspector.startSession("wg-sgp", "wg-sv-sgp")
	if err != nil {
		t.Fatalf("startSession failed: %v", err)
	}
	if _, err := inspector.updateAndSnapshot("wg-other", sessionID, nil); !errors.Is(err, errFlowInspectorVPNMismatch) {
		t.Fatalf("expected vpn mismatch error, got %v", err)
	}
}
