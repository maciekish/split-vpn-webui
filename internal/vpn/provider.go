package vpn

// Provider is the strategy interface for VPN-type specific behavior.
type Provider interface {
	Type() string
	ValidateConfig(raw string) error
	ParseConfig(raw string) (*VPNProfile, error)
	GenerateUnit(profile *VPNProfile, dataDir string) string
}
