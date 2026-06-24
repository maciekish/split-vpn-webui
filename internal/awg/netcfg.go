package awg

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Runner executes external commands. It exists so tests can record commands
// without touching the host network stack.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
}

// ExecRunner runs commands via exec.Command with explicit argument slices.
type ExecRunner struct {
	Logf func(format string, args ...any)
}

func (r *ExecRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, trimmed)
		}
		return fmt.Errorf("%s %s: %v", name, strings.Join(args, " "), err)
	}
	if r.Logf != nil && len(output) > 0 {
		r.Logf("%s %s: %s", name, strings.Join(args, " "), strings.TrimSpace(string(output)))
	}
	return nil
}

// ConfigureInterface assigns addresses, sets the MTU, brings the link up, and
// installs AllowedIPs routes into the tunnel's route table. It is idempotent:
// `replace` semantics are used throughout so a retry never fails on
// already-present state.
func ConfigureInterface(ctx context.Context, runner Runner, spec *TunnelSpec) error {
	iface := spec.InterfaceName
	if err := runner.Run(ctx, "ip", "link", "set", "dev", iface, "mtu", strconv.Itoa(spec.MTU), "up"); err != nil {
		return err
	}
	for _, addr := range spec.Addresses {
		if err := runner.Run(ctx, "ip", ipFamilyFlag(addr), "addr", "replace", addr, "dev", iface); err != nil {
			return err
		}
	}
	table := strconv.Itoa(spec.RouteTable)
	for _, peer := range spec.Peers {
		for _, allowed := range peer.AllowedIPs {
			if err := runner.Run(ctx, "ip", ipFamilyFlag(allowed), "route", "replace", allowed, "dev", iface, "table", table); err != nil {
				return err
			}
		}
	}
	return nil
}

// FlushRoutes removes this tunnel's routes from its route table. Errors are
// returned joined but all families are attempted; an empty table is not an
// error. The route table is exclusively allocated to this VPN, so flushing
// the whole table is safe.
func FlushRoutes(ctx context.Context, runner Runner, spec *TunnelSpec) error {
	table := strconv.Itoa(spec.RouteTable)
	var errs []string
	for _, family := range []string{"-4", "-6"} {
		if err := runner.Run(ctx, "ip", family, "route", "flush", "table", table); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("route flush: %s", strings.Join(errs, "; "))
	}
	return nil
}

// DeleteLink removes a kernel-managed link. Userspace TUN devices disappear
// when the owning process closes them, so this is only used by the kernel
// backend.
func DeleteLink(ctx context.Context, runner Runner, iface string) error {
	return runner.Run(ctx, "ip", "link", "del", "dev", iface)
}

func ipFamilyFlag(cidr string) string {
	if strings.Contains(cidr, ":") {
		return "-6"
	}
	return "-4"
}
