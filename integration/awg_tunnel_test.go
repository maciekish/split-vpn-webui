//go:build integration

package integration

// Run on a Linux host with root and /dev/net/tun:
//
//	SPLITVPNWEBUI_RUN_INTEGRATION=1 go test -tags integration -run TestIntegrationAmneziaWGUserspaceTunnel ./integration/
//
// The test brings up a real userspace AmneziaWG tunnel against an
// unreachable endpoint: no handshake is expected, only correct device,
// address, and route programming.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"split-vpn-webui/internal/awg"
	"split-vpn-webui/internal/vpn"
)

func TestIntegrationAmneziaWGUserspaceTunnel(t *testing.T) {
	if os.Getenv("SPLITVPNWEBUI_RUN_INTEGRATION") != "1" {
		t.Skip("set SPLITVPNWEBUI_RUN_INTEGRATION=1 to run integration tests")
	}
	if os.Geteuid() != 0 {
		t.Skip("integration test requires root privileges")
	}
	if _, err := os.Stat("/dev/net/tun"); err != nil {
		t.Skipf("/dev/net/tun unavailable: %v", err)
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	keyB64 := base64.StdEncoding.EncodeToString(key)

	config := `[Interface]
PrivateKey = ` + keyB64 + `
Address = 10.251.0.2/32
Jc = 2
Jmin = 40
Jmax = 70
S1 = 16
S2 = 32
H1 = 1086373345
H2 = 1086373346
H3 = 1086373347
H4 = 1086373348

[Peer]
PublicKey = ` + keyB64 + `
AllowedIPs = 192.0.2.0/24
Endpoint = 192.0.2.1:51820
PersistentKeepalive = 25
`
	profile, err := vpn.NewAmneziaWGProvider().ParseConfig(config)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	profile.Name = "itest"
	profile.InterfaceName = "awg-sv-itest"
	profile.RouteTable = 251

	spec, err := awg.BuildSpec(profile)
	if err != nil {
		t.Fatalf("build spec: %v", err)
	}
	deps := &awg.BackendDeps{
		Runner: &awg.ExecRunner{Logf: t.Logf},
		Logf:   t.Logf,
		// Force userspace regardless of host modules.
		SysModulePath: "/nonexistent/amneziawg",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	backend, err := awg.SelectBackend(ctx, spec, deps)
	if err != nil {
		t.Fatalf("select backend: %v", err)
	}
	if backend.Name() != "userspace" {
		t.Fatalf("expected userspace backend, got %s", backend.Name())
	}
	if err := backend.Up(ctx, spec); err != nil {
		t.Fatalf("tunnel up: %v", err)
	}
	defer func() {
		if err := backend.Down(context.Background(), spec); err != nil {
			t.Errorf("tunnel down: %v", err)
		}
		if output, err := exec.Command("ip", "link", "show", "dev", spec.InterfaceName).CombinedOutput(); err == nil {
			t.Errorf("interface still present after Down: %s", output)
		}
	}()

	link, err := exec.Command("ip", "addr", "show", "dev", spec.InterfaceName).CombinedOutput()
	if err != nil {
		t.Fatalf("interface missing: %v: %s", err, link)
	}
	if !strings.Contains(string(link), "10.251.0.2") {
		t.Fatalf("address not assigned:\n%s", link)
	}
	routes, err := exec.Command("ip", "route", "show", "table", "251").CombinedOutput()
	if err != nil {
		t.Fatalf("route table read failed: %v: %s", err, routes)
	}
	if !strings.Contains(string(routes), "192.0.2.0/24") {
		t.Fatalf("AllowedIPs route missing from table 251:\n%s", routes)
	}
}
