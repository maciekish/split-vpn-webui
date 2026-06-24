package awg

import (
	"context"
	"fmt"
	"os"
)

// Backend brings an AmneziaWG tunnel up and down through a specific engine.
type Backend interface {
	// Name identifies the backend for logs ("userspace" or "kernel").
	Name() string
	// Up brings the tunnel up. It must clean up after itself on failure.
	Up(ctx context.Context, spec *TunnelSpec) error
	// Down tears the tunnel down.
	Down(ctx context.Context, spec *TunnelSpec) error
	// Dead is closed if the tunnel dies on its own (userspace device
	// failure). May return nil when the backend cannot detect this.
	Dead() <-chan struct{}
}

// BackendDeps captures host probes so tests can fake module availability.
type BackendDeps struct {
	Runner Runner
	Logf   func(format string, args ...any)
	// SysModulePath is the sysfs directory that exists when the amneziawg
	// kernel module is loaded. Defaults to /sys/module/amneziawg.
	SysModulePath string
}

func (d *BackendDeps) logf(format string, args ...any) {
	if d.Logf != nil {
		d.Logf(format, args...)
	}
}

func (d *BackendDeps) sysModulePath() string {
	if d.SysModulePath != "" {
		return d.SysModulePath
	}
	return "/sys/module/amneziawg"
}

// kernelModuleAvailable reports whether the amneziawg kernel module is loaded
// or loadable. The probe runs at every tunnel start so a module installed
// after the profile was created is picked up.
func (d *BackendDeps) kernelModuleAvailable(ctx context.Context) bool {
	if _, err := os.Stat(d.sysModulePath()); err == nil {
		return true
	}
	if err := d.Runner.Run(ctx, "modprobe", "-q", "amneziawg"); err != nil {
		return false
	}
	_, err := os.Stat(d.sysModulePath())
	return err == nil
}

// SelectBackend picks the engine for this tunnel based on kernel module
// availability and the profile's parameter set:
//   - S3/S4 padding and H1-H4 range syntax need the kernel module.
//   - J1-J3/Itime need the bundled userspace engine.
//   - Otherwise the kernel module wins when available (better throughput).
func SelectBackend(ctx context.Context, spec *TunnelSpec, deps *BackendDeps) (Backend, error) {
	needsKernel := spec.Params.UsesExtendedPadding() || spec.Params.UsesHeaderRanges()
	needsUserspace := spec.Params.UsesUserspaceOnlyJunk()
	if needsKernel && needsUserspace {
		return nil, fmt.Errorf("config combines S3/S4 or H1-H4 ranges (kernel module only) with J1-J3/Itime (userspace only); no engine supports both")
	}

	moduleAvailable := deps.kernelModuleAvailable(ctx)
	switch {
	case needsKernel && !moduleAvailable:
		return nil, fmt.Errorf("config uses S3/S4 padding or H1-H4 ranges, which require the amneziawg kernel module, but the module is not available on this system")
	case needsKernel:
		return newKernelBackend(deps), nil
	case needsUserspace:
		if moduleAvailable {
			deps.logf("amneziawg kernel module is available but config uses J1-J3/Itime, which it does not support; using the userspace engine")
		}
		return newUserspaceBackend(deps), nil
	case moduleAvailable:
		return newKernelBackend(deps), nil
	default:
		return newUserspaceBackend(deps), nil
	}
}
