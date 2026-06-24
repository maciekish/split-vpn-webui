package awg

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"runtime"

	"github.com/amnezia-vpn/amneziawg-go/conn"
	"github.com/amnezia-vpn/amneziawg-go/device"
	"github.com/amnezia-vpn/amneziawg-go/ipc"
	"github.com/amnezia-vpn/amneziawg-go/tun"
)

// userspaceBackend runs the tunnel through the embedded amneziawg-go engine.
type userspaceBackend struct {
	deps *BackendDeps

	dev      *device.Device
	uapiSock net.Listener
}

func newUserspaceBackend(deps *BackendDeps) *userspaceBackend {
	return &userspaceBackend{deps: deps}
}

func (b *userspaceBackend) Name() string { return "userspace" }

func (b *userspaceBackend) Up(ctx context.Context, spec *TunnelSpec) error {
	if runtime.GOOS == "linux" {
		if _, err := os.Stat("/dev/net/tun"); err != nil {
			return fmt.Errorf("/dev/net/tun is not available: %v", err)
		}
	}

	tdev, err := tun.CreateTUN(spec.InterfaceName, spec.MTU)
	if err != nil {
		return fmt.Errorf("create TUN device %s: %v", spec.InterfaceName, err)
	}
	cleanup := func() {
		if b.uapiSock != nil {
			_ = b.uapiSock.Close()
			b.uapiSock = nil
		}
		if b.dev != nil {
			b.dev.Close()
			b.dev = nil
		} else {
			_ = tdev.Close()
		}
	}

	logLevel := device.LogLevelError
	if os.Getenv("LOG_LEVEL") == "debug" {
		logLevel = device.LogLevelVerbose
	}
	logger := device.NewLogger(logLevel, fmt.Sprintf("(%s) ", spec.InterfaceName))
	b.dev = device.NewDevice(tdev, conn.NewDefaultBind(), logger)

	// Expose the UAPI socket so `awg show`-compatible tooling and future
	// status queries can inspect the device. Failure is non-fatal.
	if uapiFile, err := ipc.UAPIOpen(spec.InterfaceName); err != nil {
		b.deps.logf("warning: failed to open UAPI socket for %s: %v", spec.InterfaceName, err)
	} else if listener, err := ipc.UAPIListen(spec.InterfaceName, uapiFile); err != nil {
		b.deps.logf("warning: failed to listen on UAPI socket for %s: %v", spec.InterfaceName, err)
		_ = uapiFile.Close()
	} else {
		b.uapiSock = listener
		go b.serveUAPI(listener)
	}

	endpoints, err := resolvePeerEndpoints(ctx, spec, b.deps.logf)
	if err != nil {
		cleanup()
		return err
	}
	uapiConfig, err := BuildUAPI(spec, endpoints)
	if err != nil {
		cleanup()
		return err
	}
	if err := b.dev.IpcSet(uapiConfig); err != nil {
		cleanup()
		return fmt.Errorf("configure device: %v", err)
	}
	if err := b.dev.Up(); err != nil {
		cleanup()
		return fmt.Errorf("bring device up: %v", err)
	}
	if err := ConfigureInterface(ctx, b.deps.Runner, spec); err != nil {
		cleanup()
		return err
	}
	return nil
}

func (b *userspaceBackend) serveUAPI(listener net.Listener) {
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		dev := b.dev
		if dev == nil {
			_ = connection.Close()
			return
		}
		go dev.IpcHandle(connection)
	}
}

func (b *userspaceBackend) Down(ctx context.Context, spec *TunnelSpec) error {
	if b.uapiSock != nil {
		_ = b.uapiSock.Close()
		b.uapiSock = nil
	}
	if b.dev != nil {
		b.dev.Close()
		b.dev = nil
	}
	// Closing the TUN device removes the interface and its routes; flush the
	// table anyway so a wedged kernel state never leaks stale routes.
	if err := FlushRoutes(ctx, b.deps.Runner, spec); err != nil {
		b.deps.logf("warning: %v", err)
	}
	return nil
}

func (b *userspaceBackend) Dead() <-chan struct{} {
	if b.dev == nil {
		return nil
	}
	return b.dev.Wait()
}

// resolvePeerEndpoints resolves every peer endpoint, retrying until the
// context is cancelled.
func resolvePeerEndpoints(ctx context.Context, spec *TunnelSpec, logf func(format string, args ...any)) ([]netip.AddrPort, error) {
	endpoints := make([]netip.AddrPort, 0, len(spec.Peers))
	for i, peer := range spec.Peers {
		resolved, err := ResolveEndpoint(ctx, peer.Endpoint, logf)
		if err != nil {
			return nil, fmt.Errorf("peer %d: %v", i+1, err)
		}
		endpoints = append(endpoints, resolved)
	}
	return endpoints, nil
}
