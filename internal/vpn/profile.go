package vpn

// VPNMeta stores key-value metadata persisted in vpn.conf.
type VPNMeta map[string]string

// VPNProfile is a normalized representation of a managed VPN profile.
type VPNProfile struct {
	Name           string           `json:"name"`
	Type           string           `json:"type"`
	RawConfig      string           `json:"rawConfig"`
	ConfigFile     string           `json:"configFile"`
	RouteTable     int              `json:"routeTable"`
	FWMark         uint32           `json:"fwMark"`
	InterfaceName  string           `json:"interfaceName"`
	Gateway        string           `json:"gateway"`
	BoundInterface string           `json:"boundInterface"`
	Meta           VPNMeta          `json:"meta"`
	Warnings       []string         `json:"warnings,omitempty"`
	WireGuard      *WireGuardConfig `json:"wireguard,omitempty"`
	OpenVPN        *OpenVPNConfig   `json:"openvpn,omitempty"`
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
