package server

import "testing"

func TestParseConntrackLineParsesTCP(t *testing.T) {
	line := "tcp      6 431999 ESTABLISHED src=10.0.1.10 dst=142.250.74.14 sport=50432 dport=443 packets=30 bytes=10240 src=142.250.74.14 dst=10.0.1.10 sport=443 dport=50432 packets=26 bytes=20480 [ASSURED] mark=0x1a"
	sample, ok := parseConntrackLine(line)
	if !ok {
		t.Fatalf("expected parsed sample")
	}
	if sample.Protocol != "tcp" {
		t.Fatalf("expected tcp protocol, got %q", sample.Protocol)
	}
	if sample.SourceIP != "10.0.1.10" || sample.DestinationIP != "142.250.74.14" {
		t.Fatalf("unexpected endpoints: %#v", sample)
	}
	if sample.SourcePort != 50432 || sample.DestinationPort != 443 {
		t.Fatalf("unexpected ports: %#v", sample)
	}
	if sample.UploadBytes != 10240 || sample.DownloadBytes != 20480 {
		t.Fatalf("unexpected byte counters: %#v", sample)
	}
	if sample.Mark != 0x1a {
		t.Fatalf("unexpected mark: %#x", sample.Mark)
	}
}

func TestParseConntrackLineRejectsNonTCPUDP(t *testing.T) {
	line := "icmp     1 20 src=10.0.1.10 dst=1.1.1.1 type=8 code=0 id=99 src=1.1.1.1 dst=10.0.1.10 type=0 code=0 id=99 mark=0 use=1"
	if _, ok := parseConntrackLine(line); ok {
		t.Fatalf("expected non-tcp/udp flow to be rejected")
	}
}

func TestParseConntrackLineParsesPrefixedFamilyOutput(t *testing.T) {
	line := "ipv4     2 tcp      6 431999 ESTABLISHED src=10.0.1.10 dst=142.250.74.14 sport=50432 dport=443 packets=30 bytes=10240 src=142.250.74.14 dst=10.0.1.10 sport=443 dport=50432 packets=26 bytes=20480 mark=0x1a use=1"
	sample, ok := parseConntrackLine(line)
	if !ok {
		t.Fatalf("expected parsed sample for ipv4-prefixed conntrack output")
	}
	if sample.Protocol != "tcp" {
		t.Fatalf("expected tcp protocol, got %q", sample.Protocol)
	}
	if sample.SourceIP != "10.0.1.10" || sample.DestinationIP != "142.250.74.14" {
		t.Fatalf("unexpected endpoints: %#v", sample)
	}
	if sample.UploadBytes != 10240 || sample.DownloadBytes != 20480 {
		t.Fatalf("unexpected byte counters: %#v", sample)
	}
}

func TestParseConntrackLineParsesSingleTupleFlow(t *testing.T) {
	line := "udp      17 29 src=10.0.1.50 dst=8.8.8.8 sport=51000 dport=53 packets=4 bytes=620 mark=0x170 use=1"
	sample, ok := parseConntrackLine(line)
	if !ok {
		t.Fatalf("expected parsed sample for one-way flow")
	}
	if sample.Protocol != "udp" {
		t.Fatalf("expected udp protocol, got %q", sample.Protocol)
	}
	if sample.UploadBytes != 620 {
		t.Fatalf("expected upload bytes 620, got %d", sample.UploadBytes)
	}
	if sample.DownloadBytes != 0 {
		t.Fatalf("expected zero download bytes for single tuple, got %d", sample.DownloadBytes)
	}
	if sample.Mark != 0x170 {
		t.Fatalf("expected mark 0x170, got %#x", sample.Mark)
	}
}

func TestParseConntrackSnapshotDeduplicatesByFlowKey(t *testing.T) {
	raw := `
tcp      6 431999 ESTABLISHED src=10.0.1.10 dst=142.250.74.14 sport=50432 dport=443 packets=30 bytes=10240 src=142.250.74.14 dst=10.0.1.10 sport=443 dport=50432 packets=26 bytes=20480 mark=26 use=1
tcp      6 431999 ESTABLISHED src=10.0.1.10 dst=142.250.74.14 sport=50432 dport=443 packets=30 bytes=10240 src=142.250.74.14 dst=10.0.1.10 sport=443 dport=50432 packets=26 bytes=20480 mark=26 use=1
udp      17 25 src=10.0.1.55 dst=1.1.1.1 sport=53012 dport=53 packets=4 bytes=330 src=1.1.1.1 dst=10.0.1.55 sport=53 dport=53012 packets=4 bytes=620 mark=0 use=1
`
	flows := parseConntrackSnapshot(raw)
	if len(flows) != 2 {
		t.Fatalf("expected 2 unique flows, got %d", len(flows))
	}
}

func TestParseConntrackMark(t *testing.T) {
	hexValue, hexOK := parseConntrackMark("0x1a")
	if !hexOK || hexValue != 26 {
		t.Fatalf("expected 0x1a -> 26, got value=%d ok=%v", hexValue, hexOK)
	}
	decValue, decOK := parseConntrackMark("42")
	if !decOK || decValue != 42 {
		t.Fatalf("expected 42 -> 42, got value=%d ok=%v", decValue, decOK)
	}
	if _, ok := parseConntrackMark("not-a-mark"); ok {
		t.Fatalf("expected invalid mark parse to fail")
	}
}
