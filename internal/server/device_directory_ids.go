package server

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

func parseDeviceIdentifierMAC(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if direct := normalizeMAC(trimmed); direct != "" {
		return direct
	}

	bytes, ok := parseIdentifierHexBytes(trimmed)
	if !ok || len(bytes) == 0 {
		return ""
	}

	if len(bytes) == 6 {
		return normalizeMACBytes(bytes)
	}

	// DHCPv4 client identifier:
	// type(1 byte: 0x01=Ethernet) + MAC(6 bytes)
	if len(bytes) == 7 && bytes[0] == 0x01 {
		return normalizeMACBytes(bytes[1:])
	}

	// DHCPv6 DUID:
	//   type=1 (LLT): type(2) + hwtype(2) + time(4) + lladdr
	//   type=3 (LL):  type(2) + hwtype(2) + lladdr
	if len(bytes) < 10 {
		return ""
	}
	duidType := uint16(bytes[0])<<8 | uint16(bytes[1])
	hardwareType := uint16(bytes[2])<<8 | uint16(bytes[3])
	if hardwareType != 0x0001 {
		return ""
	}
	switch duidType {
	case 0x0001, 0x0003:
		return normalizeMACBytes(bytes[len(bytes)-6:])
	default:
		return ""
	}
}

func parseIdentifierHexBytes(raw string) ([]byte, bool) {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return !unicode.Is(unicode.ASCII_Hex_Digit, r)
	})
	if len(parts) == 0 {
		return nil, false
	}
	out := make([]byte, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		if value == "" || len(value) > 2 {
			return nil, false
		}
		parsed, err := strconv.ParseUint(value, 16, 8)
		if err != nil {
			return nil, false
		}
		out = append(out, byte(parsed))
	}
	return out, true
}

func normalizeMACBytes(bytes []byte) string {
	if len(bytes) != 6 {
		return ""
	}
	candidate := fmt.Sprintf(
		"%02x:%02x:%02x:%02x:%02x:%02x",
		bytes[0],
		bytes[1],
		bytes[2],
		bytes[3],
		bytes[4],
		bytes[5],
	)
	return normalizeMAC(candidate)
}
