package awg

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"split-vpn-webui/internal/vpn"
)

type fakeRunner struct {
	calls []string
	errs  map[string]error
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	if err, ok := f.errs[call]; ok {
		return err
	}
	return nil
}

func testKey(fill byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{fill}, 32))
}

func testProfile(t *testing.T, extraInterfaceLines string) *vpn.VPNProfile {
	t.Helper()
	config := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = 10.49.1.2/32, 2001:db8::2/128
%s

[Peer]
PublicKey = %s
PresharedKey = %s
AllowedIPs = 0.0.0.0/0, ::/0
Endpoint = 192.0.2.10:51820
PersistentKeepalive = 25
`, testKey(1), extraInterfaceLines, testKey(2), testKey(3))
	profile, err := vpn.NewAmneziaWGProvider().ParseConfig(config)
	if err != nil {
		t.Fatalf("ParseConfig failed: %v", err)
	}
	profile.Name = "sgp"
	profile.InterfaceName = "awg-sv-sgp"
	profile.RouteTable = 201
	return profile
}

const awgParamLines = `Jc = 3
Jmin = 50
Jmax = 1000
S1 = 68
S2 = 149
H1 = 1086373345
H2 = 1086373346
H3 = 1086373347
H4 = 1086373348`

func TestBuildSpec(t *testing.T) {
	spec, err := BuildSpec(testProfile(t, awgParamLines+"\nMTU = 1380\nListenPort = 51821"))
	if err != nil {
		t.Fatalf("BuildSpec failed: %v", err)
	}
	if spec.InterfaceName != "awg-sv-sgp" || spec.RouteTable != 201 {
		t.Fatalf("unexpected spec identity: %+v", spec)
	}
	if spec.MTU != 1380 {
		t.Fatalf("expected MTU 1380, got %d", spec.MTU)
	}
	if spec.ListenPort != 51821 {
		t.Fatalf("expected ListenPort 51821, got %d", spec.ListenPort)
	}
	if len(spec.Addresses) != 2 || spec.Addresses[0] != "10.49.1.2/32" {
		t.Fatalf("unexpected addresses: %v", spec.Addresses)
	}
	if len(spec.Peers) != 1 || spec.Peers[0].PersistentKeepalive != 25 {
		t.Fatalf("unexpected peers: %+v", spec.Peers)
	}
}

func TestBuildSpecRejectsBadInput(t *testing.T) {
	profile := testProfile(t, "")
	profile.RouteTable = 0
	if _, err := BuildSpec(profile); err == nil || !strings.Contains(err.Error(), "route table") {
		t.Fatalf("expected route table error, got %v", err)
	}
	profile = testProfile(t, "")
	profile.Type = "wireguard"
	if _, err := BuildSpec(profile); err == nil || !strings.Contains(err.Error(), "expected amneziawg") {
		t.Fatalf("expected type error, got %v", err)
	}
	profile = testProfile(t, "MTU = 100")
	if _, err := BuildSpec(profile); err == nil || !strings.Contains(err.Error(), "MTU") {
		t.Fatalf("expected MTU error, got %v", err)
	}
}

func TestNormalizeAddress(t *testing.T) {
	cases := map[string]string{
		"10.0.0.1":       "10.0.0.1/32",
		"10.0.0.0/24":    "10.0.0.0/24",
		"2001:db8::1":    "2001:db8::1/128",
		"2001:db8::/64":  "2001:db8::/64",
		" 192.0.2.5/32 ": "192.0.2.5/32",
	}
	for input, want := range cases {
		got, err := normalizeAddress(input)
		if err != nil {
			t.Fatalf("normalizeAddress(%q) failed: %v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeAddress(%q) = %q, want %q", input, got, want)
		}
	}
	for _, invalid := range []string{"", "not-an-ip", "10.0.0.1/33"} {
		if _, err := normalizeAddress(invalid); err == nil {
			t.Fatalf("expected error for %q", invalid)
		}
	}
}

func TestBuildUAPI(t *testing.T) {
	spec, err := BuildSpec(testProfile(t, awgParamLines+"\nI1 = <b 0xf6ab3267fa><r 16>\nItime = 90"))
	if err != nil {
		t.Fatalf("BuildSpec failed: %v", err)
	}
	endpoint := netip.MustParseAddrPort("192.0.2.10:51820")
	uapi, err := BuildUAPI(spec, []netip.AddrPort{endpoint})
	if err != nil {
		t.Fatalf("BuildUAPI failed: %v", err)
	}
	for _, want := range []string{
		"private_key=" + strings.Repeat("01", 32),
		"jc=3", "jmin=50", "jmax=1000", "s1=68", "s2=149",
		"h1=1086373345", "h4=1086373348",
		"i1=<b 0xf6ab3267fa><r 16>",
		"itime=90",
		"replace_peers=true",
		"public_key=" + strings.Repeat("02", 32),
		"preshared_key=" + strings.Repeat("03", 32),
		"endpoint=192.0.2.10:51820",
		"persistent_keepalive_interval=25",
		"replace_allowed_ips=true",
		"allowed_ip=0.0.0.0/0",
		"allowed_ip=::/0",
	} {
		if !strings.Contains(uapi, want+"\n") {
			t.Fatalf("UAPI missing %q:\n%s", want, uapi)
		}
	}
	// Device-level AWG keys must precede the first peer section.
	if strings.Index(uapi, "jc=") > strings.Index(uapi, "public_key=") {
		t.Fatalf("AWG params must come before peers:\n%s", uapi)
	}
}

func TestBuildUAPIRejectsExtendedPadding(t *testing.T) {
	spec, err := BuildSpec(testProfile(t, "S3 = 10"))
	if err != nil {
		t.Fatalf("BuildSpec failed: %v", err)
	}
	endpoint := netip.MustParseAddrPort("192.0.2.10:51820")
	if _, err := BuildUAPI(spec, []netip.AddrPort{endpoint}); err == nil || !strings.Contains(err.Error(), "S3/S4") {
		t.Fatalf("expected S3/S4 rejection, got %v", err)
	}
}

func TestBuildUAPIRejectsHeaderRanges(t *testing.T) {
	spec, err := BuildSpec(testProfile(t, "H1 = 10-20\nH2 = 30-40\nH3 = 50-60\nH4 = 70-80"))
	if err != nil {
		t.Fatalf("BuildSpec failed: %v", err)
	}
	endpoint := netip.MustParseAddrPort("192.0.2.10:51820")
	if _, err := BuildUAPI(spec, []netip.AddrPort{endpoint}); err == nil || !strings.Contains(err.Error(), "H1-H4") {
		t.Fatalf("expected H1-H4 range rejection, got %v", err)
	}
}

func TestBuildUAPIRejectsBadKeys(t *testing.T) {
	spec, err := BuildSpec(testProfile(t, ""))
	if err != nil {
		t.Fatalf("BuildSpec failed: %v", err)
	}
	spec.PrivateKey = "not-base64!!"
	endpoint := netip.MustParseAddrPort("192.0.2.10:51820")
	if _, err := BuildUAPI(spec, []netip.AddrPort{endpoint}); err == nil || !strings.Contains(err.Error(), "PrivateKey") {
		t.Fatalf("expected PrivateKey error, got %v", err)
	}
	spec.PrivateKey = base64.StdEncoding.EncodeToString([]byte("short"))
	if _, err := BuildUAPI(spec, []netip.AddrPort{endpoint}); err == nil || !strings.Contains(err.Error(), "32 bytes") {
		t.Fatalf("expected key length error, got %v", err)
	}
}

func TestConfigureInterfaceCommands(t *testing.T) {
	spec, err := BuildSpec(testProfile(t, ""))
	if err != nil {
		t.Fatalf("BuildSpec failed: %v", err)
	}
	runner := &fakeRunner{}
	if err := ConfigureInterface(context.Background(), runner, spec); err != nil {
		t.Fatalf("ConfigureInterface failed: %v", err)
	}
	want := []string{
		"ip link set dev awg-sv-sgp mtu 1420 up",
		"ip -4 addr replace 10.49.1.2/32 dev awg-sv-sgp",
		"ip -6 addr replace 2001:db8::2/128 dev awg-sv-sgp",
		"ip -4 route replace 0.0.0.0/0 dev awg-sv-sgp table 201",
		"ip -6 route replace ::/0 dev awg-sv-sgp table 201",
	}
	if len(runner.calls) != len(want) {
		t.Fatalf("expected %d commands, got %v", len(want), runner.calls)
	}
	for i, call := range want {
		if runner.calls[i] != call {
			t.Fatalf("command %d = %q, want %q", i, runner.calls[i], call)
		}
	}
}

func TestFlushRoutes(t *testing.T) {
	spec := &TunnelSpec{InterfaceName: "awg-sv-x", RouteTable: 207}
	runner := &fakeRunner{errs: map[string]error{
		"ip -6 route flush table 207": errors.New("no table"),
	}}
	err := FlushRoutes(context.Background(), runner, spec)
	if err == nil || !strings.Contains(err.Error(), "no table") {
		t.Fatalf("expected joined error, got %v", err)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("expected both families attempted, got %v", runner.calls)
	}
}

func TestSplitHookCommand(t *testing.T) {
	args, err := splitHookCommand(`ip route add "10.0.0.0/8" dev awg-sv-sgp`)
	if err != nil {
		t.Fatalf("splitHookCommand failed: %v", err)
	}
	want := []string{"ip", "route", "add", "10.0.0.0/8", "dev", "awg-sv-sgp"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected args: %#v", args)
	}

	args, err = splitHookCommand(`printf 'hello world' \> ignored`)
	if err != nil {
		t.Fatalf("splitHookCommand with quotes failed: %v", err)
	}
	want = []string{"printf", "hello world", ">", "ignored"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("unexpected quoted args: %#v", args)
	}

	for _, command := range []string{"", `"unterminated`, `trailing\`} {
		if _, err := splitHookCommand(command); err == nil {
			t.Fatalf("expected invalid command %q to fail", command)
		}
	}
}

func TestSelectBackend(t *testing.T) {
	ctx := context.Background()
	moduleDir := filepath.Join(t.TempDir(), "amneziawg")
	if err := os.MkdirAll(moduleDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	missingDir := filepath.Join(t.TempDir(), "missing")

	baseSpec := func(lines string) *TunnelSpec {
		spec, err := BuildSpec(testProfile(t, lines))
		if err != nil {
			t.Fatalf("BuildSpec failed: %v", err)
		}
		return spec
	}

	t.Run("module present picks kernel", func(t *testing.T) {
		deps := &BackendDeps{Runner: &fakeRunner{}, SysModulePath: moduleDir}
		backend, err := SelectBackend(ctx, baseSpec(awgParamLines), deps)
		if err != nil || backend.Name() != "kernel" {
			t.Fatalf("expected kernel backend, got %v / %v", backend, err)
		}
	})

	t.Run("module absent picks userspace", func(t *testing.T) {
		runner := &fakeRunner{errs: map[string]error{"modprobe -q amneziawg": errors.New("not found")}}
		deps := &BackendDeps{Runner: runner, SysModulePath: missingDir}
		backend, err := SelectBackend(ctx, baseSpec(awgParamLines), deps)
		if err != nil || backend.Name() != "userspace" {
			t.Fatalf("expected userspace backend, got %v / %v", backend, err)
		}
		if len(runner.calls) == 0 || runner.calls[0] != "modprobe -q amneziawg" {
			t.Fatalf("expected modprobe attempt, got %v", runner.calls)
		}
	})

	t.Run("signature packets use kernel when module is present", func(t *testing.T) {
		deps := &BackendDeps{Runner: &fakeRunner{}, SysModulePath: moduleDir}
		backend, err := SelectBackend(ctx, baseSpec("I1 = <r 32>"), deps)
		if err != nil || backend.Name() != "kernel" {
			t.Fatalf("expected kernel backend, got %v / %v", backend, err)
		}
	})

	t.Run("controlled junk forces userspace despite module", func(t *testing.T) {
		deps := &BackendDeps{Runner: &fakeRunner{}, SysModulePath: moduleDir}
		backend, err := SelectBackend(ctx, baseSpec("J1 = <r 32>"), deps)
		if err != nil || backend.Name() != "userspace" {
			t.Fatalf("expected userspace backend, got %v / %v", backend, err)
		}
	})

	t.Run("extended padding requires module", func(t *testing.T) {
		runner := &fakeRunner{errs: map[string]error{"modprobe -q amneziawg": errors.New("not found")}}
		deps := &BackendDeps{Runner: runner, SysModulePath: missingDir}
		_, err := SelectBackend(ctx, baseSpec("S3 = 10"), deps)
		if err == nil || !strings.Contains(err.Error(), "kernel module") {
			t.Fatalf("expected kernel module requirement error, got %v", err)
		}
	})

	t.Run("extended padding with module picks kernel", func(t *testing.T) {
		deps := &BackendDeps{Runner: &fakeRunner{}, SysModulePath: moduleDir}
		backend, err := SelectBackend(ctx, baseSpec("S4 = 8"), deps)
		if err != nil || backend.Name() != "kernel" {
			t.Fatalf("expected kernel backend, got %v / %v", backend, err)
		}
	})

	t.Run("header ranges require module", func(t *testing.T) {
		runner := &fakeRunner{errs: map[string]error{"modprobe -q amneziawg": errors.New("not found")}}
		deps := &BackendDeps{Runner: runner, SysModulePath: missingDir}
		_, err := SelectBackend(ctx, baseSpec("H1 = 10-20\nH2 = 30-40\nH3 = 50-60\nH4 = 70-80"), deps)
		if err == nil || !strings.Contains(err.Error(), "kernel module") {
			t.Fatalf("expected kernel module requirement error, got %v", err)
		}
	})

	t.Run("header ranges with module pick kernel", func(t *testing.T) {
		deps := &BackendDeps{Runner: &fakeRunner{}, SysModulePath: moduleDir}
		backend, err := SelectBackend(ctx, baseSpec("H1 = 10-20\nH2 = 30-40\nH3 = 50-60\nH4 = 70-80"), deps)
		if err != nil || backend.Name() != "kernel" {
			t.Fatalf("expected kernel backend, got %v / %v", backend, err)
		}
	})
}

func TestResolveEndpointLiteral(t *testing.T) {
	resolved, err := ResolveEndpoint(context.Background(), "192.0.2.10:51820", nil)
	if err != nil {
		t.Fatalf("ResolveEndpoint failed: %v", err)
	}
	if resolved.String() != "192.0.2.10:51820" {
		t.Fatalf("unexpected endpoint %s", resolved)
	}
	if _, err := ResolveEndpoint(context.Background(), "no-port-here", nil); err == nil {
		t.Fatalf("expected error for endpoint without port")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ResolveEndpoint(ctx, "[2001:db8::1]:51820", nil); err != nil {
		t.Fatalf("IPv6 literal should not need resolution: %v", err)
	}
}
