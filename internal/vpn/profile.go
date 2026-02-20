package vpn

// VPNMeta stores key-value metadata persisted in vpn.conf.
type VPNMeta map[string]string

// VPNProfile is a normalized representation of a managed VPN profile.
type VPNProfile struct {
	Name           string
	Type           string
	RawConfig      string
	ConfigFile     string
	RouteTable     int
	FWMark         uint32
	InterfaceName  string
	Gateway        string
	BoundInterface string
	Meta           VPNMeta
	WireGuard      *WireGuardConfig
	OpenVPN        *OpenVPNConfig
}

// WireGuardConfig captures parsed fields from a WireGuard config.
type WireGuardConfig struct {
	Interface WireGuardInterface
	Peers     []WireGuardPeer
}

// WireGuardInterface holds [Interface] data.
type WireGuardInterface struct {
	PrivateKey string
	Addresses  []string
	DNS        []string
	Table      string
	PostUp     []string
	PreDown    []string
	PostDown   []string
	Extras     map[string][]string
}

// WireGuardPeer holds [Peer] data.
type WireGuardPeer struct {
	PublicKey           string
	PresharedKey        string
	AllowedIPs          []string
	Endpoint            string
	PersistentKeepalive string
	Extras              map[string][]string
}

// OpenVPNConfig captures parsed OpenVPN directives and inline blocks.
type OpenVPNConfig struct {
	Directives   map[string][]string
	InlineBlocks map[string]string
}
