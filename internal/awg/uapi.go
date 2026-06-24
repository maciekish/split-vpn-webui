package awg

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/netip"
	"strings"

	"split-vpn-webui/internal/vpn"
)

// BuildUAPI renders the UAPI `set` document the userspace engine consumes.
// Endpoints must already be resolved to IP:port, in the same order as
// spec.Peers.
func BuildUAPI(spec *TunnelSpec, endpoints []netip.AddrPort) (string, error) {
	if len(endpoints) != len(spec.Peers) {
		return "", fmt.Errorf("got %d resolved endpoints for %d peers", len(endpoints), len(spec.Peers))
	}
	var b strings.Builder

	privateHex, err := keyToHex("PrivateKey", spec.PrivateKey)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "private_key=%s\n", privateHex)
	if spec.ListenPort > 0 {
		fmt.Fprintf(&b, "listen_port=%d\n", spec.ListenPort)
	}
	if err := writeUAPIParams(&b, spec.Params); err != nil {
		return "", err
	}

	b.WriteString("replace_peers=true\n")
	for i, peer := range spec.Peers {
		publicHex, err := keyToHex(fmt.Sprintf("peer %d PublicKey", i+1), peer.PublicKey)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&b, "public_key=%s\n", publicHex)
		if peer.PresharedKey != "" {
			presharedHex, err := keyToHex(fmt.Sprintf("peer %d PresharedKey", i+1), peer.PresharedKey)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&b, "preshared_key=%s\n", presharedHex)
		}
		fmt.Fprintf(&b, "endpoint=%s\n", endpoints[i].String())
		if peer.PersistentKeepalive > 0 {
			fmt.Fprintf(&b, "persistent_keepalive_interval=%d\n", peer.PersistentKeepalive)
		}
		b.WriteString("replace_allowed_ips=true\n")
		for _, allowed := range peer.AllowedIPs {
			fmt.Fprintf(&b, "allowed_ip=%s\n", allowed)
		}
	}
	return b.String(), nil
}

// writeUAPIParams emits the AmneziaWG obfuscation keys. S3/S4 and H1-H4
// range syntax are not supported by the bundled userspace engine; callers
// must route such specs to the kernel backend (enforced by SelectBackend).
func writeUAPIParams(b *strings.Builder, params *vpn.AmneziaWGParams) error {
	if params.IsEmpty() {
		return nil
	}
	if params.UsesExtendedPadding() {
		return fmt.Errorf("S3/S4 padding is not supported by the userspace engine")
	}
	if params.UsesHeaderRanges() {
		return fmt.Errorf("H1-H4 ranges are not supported by the userspace engine")
	}
	writeInt := func(key string, value *int) {
		if value != nil {
			fmt.Fprintf(b, "%s=%d\n", key, *value)
		}
	}
	writeString := func(key, value string) {
		if value != "" {
			fmt.Fprintf(b, "%s=%s\n", key, value)
		}
	}
	writeInt("jc", params.Jc)
	writeInt("jmin", params.Jmin)
	writeInt("jmax", params.Jmax)
	writeInt("s1", params.S1)
	writeInt("s2", params.S2)
	writeString("h1", params.H1)
	writeString("h2", params.H2)
	writeString("h3", params.H3)
	writeString("h4", params.H4)
	writeString("i1", params.I1)
	writeString("i2", params.I2)
	writeString("i3", params.I3)
	writeString("i4", params.I4)
	writeString("i5", params.I5)
	writeString("j1", params.J1)
	writeString("j2", params.J2)
	writeString("j3", params.J3)
	if params.ITime != nil {
		fmt.Fprintf(b, "itime=%d\n", *params.ITime)
	}
	return nil
}

func keyToHex(label, b64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return "", fmt.Errorf("%s is not valid base64: %v", label, err)
	}
	if len(raw) != 32 {
		return "", fmt.Errorf("%s must decode to 32 bytes, got %d", label, len(raw))
	}
	return hex.EncodeToString(raw), nil
}
