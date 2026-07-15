//go:build !linux

package netbind

import "syscall"

// Control is a no-op on non-Linux platforms where SO_BINDTODEVICE is
// unavailable; callers fall back to the default route.
func Control(iface string) func(network, address string, c syscall.RawConn) error {
	return nil
}
