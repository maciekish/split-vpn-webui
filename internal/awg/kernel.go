package awg

import (
	"context"
	"fmt"
	"net"
	"time"

	wgctrl "github.com/Jipok/wgctrl-go"
	"github.com/Jipok/wgctrl-go/wgtypes"
)

// kernelBackend drives the amneziawg kernel module: it creates a link of
// kind "amneziawg" and configures it over generic netlink.
type kernelBackend struct {
	deps *BackendDeps
}

func newKernelBackend(deps *BackendDeps) *kernelBackend {
	return &kernelBackend{deps: deps}
}

func (b *kernelBackend) Name() string { return "kernel" }

func (b *kernelBackend) Up(ctx context.Context, spec *TunnelSpec) error {
	// A previous crash can leave a stale link behind; remove it quietly so
	// bring-up is idempotent.
	_ = DeleteLink(ctx, b.deps.Runner, spec.InterfaceName)
	if err := b.deps.Runner.Run(ctx, "ip", "link", "add", spec.InterfaceName, "type", "amneziawg"); err != nil {
		return fmt.Errorf("create amneziawg link: %v", err)
	}

	if err := b.configure(ctx, spec); err != nil {
		_ = DeleteLink(ctx, b.deps.Runner, spec.InterfaceName)
		return err
	}
	if err := ConfigureInterface(ctx, b.deps.Runner, spec); err != nil {
		_ = DeleteLink(ctx, b.deps.Runner, spec.InterfaceName)
		return err
	}
	return nil
}

func (b *kernelBackend) configure(ctx context.Context, spec *TunnelSpec) error {
	endpoints, err := resolvePeerEndpoints(ctx, spec, b.deps.logf)
	if err != nil {
		return err
	}

	privateKey, err := wgtypes.ParseKey(spec.PrivateKey)
	if err != nil {
		return fmt.Errorf("PrivateKey: %v", err)
	}
	cfg := wgtypes.Config{
		PrivateKey:   &privateKey,
		ReplacePeers: true,
	}
	if spec.ListenPort > 0 {
		port := spec.ListenPort
		cfg.ListenPort = &port
	}
	applyKernelParams(&cfg, spec)

	for i, peer := range spec.Peers {
		publicKey, err := wgtypes.ParseKey(peer.PublicKey)
		if err != nil {
			return fmt.Errorf("peer %d PublicKey: %v", i+1, err)
		}
		peerCfg := wgtypes.PeerConfig{
			PublicKey:         publicKey,
			ReplaceAllowedIPs: true,
			Endpoint: &net.UDPAddr{
				IP:   endpoints[i].Addr().AsSlice(),
				Port: int(endpoints[i].Port()),
			},
		}
		if peer.PresharedKey != "" {
			presharedKey, err := wgtypes.ParseKey(peer.PresharedKey)
			if err != nil {
				return fmt.Errorf("peer %d PresharedKey: %v", i+1, err)
			}
			peerCfg.PresharedKey = &presharedKey
		}
		if peer.PersistentKeepalive > 0 {
			interval := time.Duration(peer.PersistentKeepalive) * time.Second
			peerCfg.PersistentKeepaliveInterval = &interval
		}
		for _, allowed := range peer.AllowedIPs {
			_, ipNet, err := net.ParseCIDR(allowed)
			if err != nil {
				return fmt.Errorf("peer %d AllowedIPs %q: %v", i+1, allowed, err)
			}
			peerCfg.AllowedIPs = append(peerCfg.AllowedIPs, *ipNet)
		}
		cfg.Peers = append(cfg.Peers, peerCfg)
	}

	client, err := wgctrl.New()
	if err != nil {
		return fmt.Errorf("open wireguard control client: %v", err)
	}
	defer client.Close()
	if err := client.ConfigureDevice(spec.InterfaceName, cfg); err != nil {
		return fmt.Errorf("configure %s via netlink: %v", spec.InterfaceName, err)
	}
	return nil
}

// applyKernelParams maps the profile's kernel-supported obfuscation
// parameters onto the netlink config. J1-J3/Itime are userspace-only and
// never reach this backend (enforced by SelectBackend).
func applyKernelParams(cfg *wgtypes.Config, spec *TunnelSpec) {
	params := spec.Params
	if params.IsEmpty() {
		return
	}
	cfg.Jc = params.Jc
	cfg.Jmin = params.Jmin
	cfg.Jmax = params.Jmax
	cfg.S1 = params.S1
	cfg.S2 = params.S2
	cfg.S3 = params.S3
	cfg.S4 = params.S4
	stringPtr := func(value string) *string {
		if value == "" {
			return nil
		}
		return &value
	}
	cfg.H1 = stringPtr(params.H1)
	cfg.H2 = stringPtr(params.H2)
	cfg.H3 = stringPtr(params.H3)
	cfg.H4 = stringPtr(params.H4)
	cfg.I1 = stringPtr(params.I1)
	cfg.I2 = stringPtr(params.I2)
	cfg.I3 = stringPtr(params.I3)
	cfg.I4 = stringPtr(params.I4)
	cfg.I5 = stringPtr(params.I5)
}

func (b *kernelBackend) Down(ctx context.Context, spec *TunnelSpec) error {
	if err := FlushRoutes(ctx, b.deps.Runner, spec); err != nil {
		b.deps.logf("warning: %v", err)
	}
	return DeleteLink(ctx, b.deps.Runner, spec.InterfaceName)
}

func (b *kernelBackend) Dead() <-chan struct{} { return nil }
