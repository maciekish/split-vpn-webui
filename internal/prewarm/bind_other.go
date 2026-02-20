//go:build !linux

package prewarm

import "syscall"

func interfaceBindControl(iface string) func(network, address string, c syscall.RawConn) error {
	return nil
}
