package server

import "testing"

func TestParseDHCPLeaseRows(t *testing.T) {
	raw := `
# expiry mac ip hostname clientid
1740220000 00:11:22:33:44:55 192.168.1.20 media-player *
1740220001 00:11:22:33:44:66 192.168.1.21 * *
bad row
`
	rows := parseDHCPLeaseRows(raw)
	if len(rows) != 2 {
		t.Fatalf("expected 2 lease rows, got %d", len(rows))
	}
	if rows[0].MAC != "00:11:22:33:44:55" {
		t.Fatalf("unexpected mac normalization: %q", rows[0].MAC)
	}
	if rows[0].IP != "192.168.1.20" {
		t.Fatalf("unexpected ip normalization: %q", rows[0].IP)
	}
	if rows[0].Hostname != "media-player" {
		t.Fatalf("unexpected hostname: %q", rows[0].Hostname)
	}
	if rows[1].Hostname != "" {
		t.Fatalf("expected wildcard hostname to be blank, got %q", rows[1].Hostname)
	}
}

func TestIngestDevicePayloadMapsMACAndIP(t *testing.T) {
	directory := deviceDirectory{
		byMAC:  make(map[string]string),
		byIP:   make(map[string]string),
		ipsMAC: make(map[string]map[string]struct{}),
	}
	payload := map[string]any{
		"clients": []any{
			map[string]any{
				"macAddress":  "AA:BB:CC:DD:EE:FF",
				"displayName": "Living Room TV",
				"ipAddress":   "10.0.1.30",
			},
		},
	}
	ingestDevicePayload(payload, &directory)
	name, hints := directory.lookupMAC("aa:bb:cc:dd:ee:ff")
	if name != "Living Room TV" {
		t.Fatalf("expected mac name mapping, got %q", name)
	}
	if len(hints) != 1 || hints[0] != "10.0.1.30" {
		t.Fatalf("expected ip hints [10.0.1.30], got %#v", hints)
	}
	if directory.lookupIP("10.0.1.30") != "Living Room TV" {
		t.Fatalf("expected ip name mapping for 10.0.1.30")
	}
}

func TestDeviceDirectoryListDevicesPreservesDiscoveryOrder(t *testing.T) {
	directory := deviceDirectory{}
	directory.addMACName("00:11:22:33:44:55", "First Device")
	directory.addMACIP("00:11:22:33:44:55", "10.0.1.10")
	directory.addMACName("00:11:22:33:44:66", "Second Device")
	directory.addMACIP("00:11:22:33:44:66", "10.0.1.20")

	list := directory.listDevices()
	if len(list) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(list))
	}
	if list[0].MAC != "00:11:22:33:44:55" || list[1].MAC != "00:11:22:33:44:66" {
		t.Fatalf("expected insertion order to be preserved, got %#v", list)
	}
	if list[0].SearchText == "" || list[1].SearchText == "" {
		t.Fatalf("expected non-empty search text fields")
	}
}
