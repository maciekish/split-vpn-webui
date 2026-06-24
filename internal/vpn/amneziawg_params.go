package vpn

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/amnezia-vpn/amneziawg-go/device/awg"
)

// AmneziaWGParams captures the AmneziaWG obfuscation parameters from the
// [Interface] section of a config. All fields are optional; absent fields
// keep vanilla WireGuard wire behavior.
type AmneziaWGParams struct {
	Jc    *int   `json:"jc,omitempty"`
	Jmin  *int   `json:"jmin,omitempty"`
	Jmax  *int   `json:"jmax,omitempty"`
	S1    *int   `json:"s1,omitempty"`
	S2    *int   `json:"s2,omitempty"`
	S3    *int   `json:"s3,omitempty"`
	S4    *int   `json:"s4,omitempty"`
	H1    string `json:"h1,omitempty"`
	H2    string `json:"h2,omitempty"`
	H3    string `json:"h3,omitempty"`
	H4    string `json:"h4,omitempty"`
	I1    string `json:"i1,omitempty"`
	I2    string `json:"i2,omitempty"`
	I3    string `json:"i3,omitempty"`
	I4    string `json:"i4,omitempty"`
	I5    string `json:"i5,omitempty"`
	J1    string `json:"j1,omitempty"`
	J2    string `json:"j2,omitempty"`
	J3    string `json:"j3,omitempty"`
	ITime *int64 `json:"itime,omitempty"`
}

// awgParamKeys lists every AmneziaWG-specific [Interface] key (lowercased).
var awgParamKeys = []string{
	"jc", "jmin", "jmax",
	"s1", "s2", "s3", "s4",
	"h1", "h2", "h3", "h4",
	"i1", "i2", "i3", "i4", "i5",
	"j1", "j2", "j3",
	"itime",
}

// HasAmneziaWGKeys reports whether the parsed interface section contains any
// AmneziaWG obfuscation keys.
func HasAmneziaWGKeys(iface *WireGuardInterface) bool {
	if iface == nil || iface.Extras == nil {
		return false
	}
	for _, key := range awgParamKeys {
		if len(iface.Extras[key]) > 0 {
			return true
		}
	}
	return false
}

// IsEmpty reports whether no obfuscation parameter is set.
func (p *AmneziaWGParams) IsEmpty() bool {
	return p == nil || (p.Jc == nil && p.Jmin == nil && p.Jmax == nil &&
		p.S1 == nil && p.S2 == nil && p.S3 == nil && p.S4 == nil &&
		p.H1 == "" && p.H2 == "" && p.H3 == "" && p.H4 == "" &&
		p.I1 == "" && p.I2 == "" && p.I3 == "" && p.I4 == "" && p.I5 == "" &&
		p.J1 == "" && p.J2 == "" && p.J3 == "" && p.ITime == nil)
}

// UsesSpecialJunk reports whether v1.5 signature/controlled junk or itime is
// configured.
func (p *AmneziaWGParams) UsesSpecialJunk() bool {
	return p != nil && (p.I1 != "" || p.I2 != "" || p.I3 != "" || p.I4 != "" || p.I5 != "" ||
		p.J1 != "" || p.J2 != "" || p.J3 != "" || p.ITime != nil)
}

// UsesUserspaceOnlyJunk reports whether params unsupported by the current
// kernel netlink path are configured.
func (p *AmneziaWGParams) UsesUserspaceOnlyJunk() bool {
	return p != nil && (p.J1 != "" || p.J2 != "" || p.J3 != "" || p.ITime != nil)
}

// UsesExtendedPadding reports whether S3/S4 padding is configured. These are
// supported by the kernel module only with the bundled userspace engine.
func (p *AmneziaWGParams) UsesExtendedPadding() bool {
	return p != nil && ((p.S3 != nil && *p.S3 > 0) || (p.S4 != nil && *p.S4 > 0))
}

// UsesHeaderRanges reports whether H1-H4 use AmneziaWG 2.0 range syntax,
// which the bundled userspace engine cannot parse.
func (p *AmneziaWGParams) UsesHeaderRanges() bool {
	if p == nil {
		return false
	}
	for _, raw := range []string{p.H1, p.H2, p.H3, p.H4} {
		if strings.Contains(raw, "-") {
			return true
		}
	}
	return false
}

// parseAmneziaWGParams extracts and validates AmneziaWG parameters from the
// parsed [Interface] extras. Returns nil if no AWG keys are present.
func parseAmneziaWGParams(iface *WireGuardInterface) (*AmneziaWGParams, error) {
	if !HasAmneziaWGKeys(iface) {
		return nil, nil
	}
	params := &AmneziaWGParams{}

	intField := func(key string, target **int, min, max int) error {
		raw, err := singleExtra(iface, key)
		if err != nil || raw == "" {
			return err
		}
		value, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("%s must be an integer, got %q", strings.ToUpper(key), raw)
		}
		if value < min || value > max {
			return fmt.Errorf("%s must be between %d and %d, got %d", strings.ToUpper(key), min, max, value)
		}
		*target = &value
		return nil
	}
	stringField := func(key string, target *string) error {
		raw, err := singleExtra(iface, key)
		if err != nil || raw == "" {
			return err
		}
		*target = raw
		return nil
	}

	for _, field := range []struct {
		key      string
		target   **int
		min, max int
	}{
		{"jc", &params.Jc, 0, 1280},
		{"jmin", &params.Jmin, 0, 1280},
		{"jmax", &params.Jmax, 0, 1280},
		{"s1", &params.S1, 0, 1132},
		{"s2", &params.S2, 0, 1188},
		{"s3", &params.S3, 0, 1188},
		{"s4", &params.S4, 0, 1188},
	} {
		if err := intField(field.key, field.target, field.min, field.max); err != nil {
			return nil, err
		}
	}
	for _, field := range []struct {
		key    string
		target *string
	}{
		{"h1", &params.H1}, {"h2", &params.H2}, {"h3", &params.H3}, {"h4", &params.H4},
		{"i1", &params.I1}, {"i2", &params.I2}, {"i3", &params.I3}, {"i4", &params.I4}, {"i5", &params.I5},
		{"j1", &params.J1}, {"j2", &params.J2}, {"j3", &params.J3},
	} {
		if err := stringField(field.key, field.target); err != nil {
			return nil, err
		}
	}
	if raw, err := singleExtra(iface, "itime"); err != nil {
		return nil, err
	} else if raw != "" {
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || value < 0 {
			return nil, fmt.Errorf("Itime must be a non-negative integer, got %q", raw)
		}
		params.ITime = &value
	}

	if err := params.validate(); err != nil {
		return nil, err
	}
	return params, nil
}

func (p *AmneziaWGParams) validate() error {
	if p.Jmin != nil && p.Jmax != nil && *p.Jmin > *p.Jmax {
		return fmt.Errorf("Jmin (%d) must not exceed Jmax (%d)", *p.Jmin, *p.Jmax)
	}
	if p.Jc != nil && *p.Jc > 0 && (p.Jmin == nil || p.Jmax == nil) {
		return fmt.Errorf("Jc requires Jmin and Jmax to be set")
	}
	if p.S1 != nil && p.S2 != nil && *p.S1+56 == *p.S2 {
		return fmt.Errorf("S1 + 56 must not equal S2 (S1=%d, S2=%d); this makes handshake packets distinguishable", *p.S1, *p.S2)
	}
	if err := p.validateHeaders(); err != nil {
		return err
	}
	for key, value := range map[string]string{
		"i1": p.I1, "i2": p.I2, "i3": p.I3, "i4": p.I4, "i5": p.I5,
		"j1": p.J1, "j2": p.J2, "j3": p.J3,
	} {
		if value == "" {
			continue
		}
		if _, err := awg.Parse(key, value); err != nil {
			return fmt.Errorf("%s is not a valid junk packet definition: %v", strings.ToUpper(key), err)
		}
	}
	if p.UsesUserspaceOnlyJunk() && (p.UsesExtendedPadding() || p.UsesHeaderRanges()) {
		return fmt.Errorf("S3/S4 or H1-H4 ranges (kernel module only) cannot be combined with J1-J3/Itime (userspace engine only); no available engine supports both")
	}
	return nil
}

type awgHeaderRange struct {
	name       string
	raw        string
	start, end uint32
}

func (p *AmneziaWGParams) validateHeaders() error {
	headers := []struct {
		name string
		raw  string
	}{
		{"H1", p.H1},
		{"H2", p.H2},
		{"H3", p.H3},
		{"H4", p.H4},
	}
	ranges := []awgHeaderRange{}
	setCount := 0
	for _, header := range headers {
		if header.raw == "" {
			continue
		}
		setCount++
		headerRange, err := parseAWGHeaderRange(header.name, header.raw)
		if err != nil {
			return err
		}
		for _, existing := range ranges {
			if headerRange.overlaps(existing) {
				if headerRange.start == headerRange.end && existing.start == existing.end {
					return fmt.Errorf("%s and %s must not share the same value %d", existing.name, header.name, headerRange.start)
				}
				return fmt.Errorf("%s (%s) must not overlap %s (%s)", header.name, header.raw, existing.name, existing.raw)
			}
		}
		ranges = append(ranges, headerRange)
	}
	if setCount > 0 && setCount < 4 {
		return fmt.Errorf("H1-H4 must either all be set or all be omitted")
	}
	return nil
}

func parseAWGHeaderRange(name, raw string) (awgHeaderRange, error) {
	parts := strings.Split(raw, "-")
	if len(parts) > 2 {
		return awgHeaderRange{}, fmt.Errorf("%s must be an unsigned 32-bit integer or range, got %q", name, raw)
	}
	parsePart := func(value string) (uint32, error) {
		parsed, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
		return uint32(parsed), err
	}
	start, err := parsePart(parts[0])
	if err != nil {
		return awgHeaderRange{}, fmt.Errorf("%s must be an unsigned 32-bit integer or range, got %q", name, raw)
	}
	end := start
	if len(parts) == 2 {
		end, err = parsePart(parts[1])
		if err != nil {
			return awgHeaderRange{}, fmt.Errorf("%s must be an unsigned 32-bit integer or range, got %q", name, raw)
		}
	}
	if start > end {
		return awgHeaderRange{}, fmt.Errorf("%s range start must not exceed end, got %q", name, raw)
	}
	return awgHeaderRange{name: name, raw: raw, start: start, end: end}, nil
}

func (r awgHeaderRange) overlaps(other awgHeaderRange) bool {
	return r.start <= other.end && other.start <= r.end
}

func singleExtra(iface *WireGuardInterface, key string) (string, error) {
	values := iface.Extras[key]
	switch len(values) {
	case 0:
		return "", nil
	case 1:
		return strings.TrimSpace(values[0]), nil
	default:
		return "", fmt.Errorf("%s may only be specified once", strings.ToUpper(key))
	}
}
