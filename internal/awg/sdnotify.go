package awg

import (
	"net"
	"os"
)

// sdNotify sends a state message to the systemd notify socket, if one is
// configured for this process. Errors are intentionally swallowed: notify is
// best-effort and the tunnel must not fail because of it.
func sdNotify(state string) {
	socketPath := os.Getenv("NOTIFY_SOCKET")
	if socketPath == "" {
		return
	}
	// systemd represents abstract-namespace sockets with a leading "@".
	if socketPath[0] == '@' {
		socketPath = "\x00" + socketPath[1:]
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: socketPath, Net: "unixgram"})
	if err != nil {
		return
	}
	defer conn.Close()
	_, _ = conn.Write([]byte(state))
}
