//go:build linux

// Package netbind provides a dialer Control callback that binds outgoing
// sockets to a specific network interface via SO_BINDTODEVICE. This forces
// egress through a chosen interface (for example a VPN tunnel device),
// bypassing the policy-routing table's interface selection.
package netbind

import (
	"strings"
	"syscall"
)

// Control returns a net.Dialer.Control function that binds the socket to the
// named interface. It returns nil when iface is empty, so callers can assign
// the result directly and leave the dialer unbound for the default route.
func Control(iface string) func(network, address string, c syscall.RawConn) error {
	trimmed := strings.TrimSpace(iface)
	if trimmed == "" {
		return nil
	}
	return func(network, address string, c syscall.RawConn) error {
		var bindErr error
		if err := c.Control(func(fd uintptr) {
			bindErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, trimmed)
		}); err != nil {
			return err
		}
		return bindErr
	}
}
