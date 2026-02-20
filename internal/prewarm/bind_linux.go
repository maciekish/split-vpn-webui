//go:build linux

package prewarm

import (
	"strings"
	"syscall"
)

func interfaceBindControl(iface string) func(network, address string, c syscall.RawConn) error {
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
