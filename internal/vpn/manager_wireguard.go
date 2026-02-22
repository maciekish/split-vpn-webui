package vpn

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

var (
	resolvconfCheckOnce sync.Once
	resolvconfAvailable bool
)

func sanitizeWireGuardConfig(raw string, routeTable int, dnsSupported bool) (string, []string, error) {
	lines := strings.Split(raw, "\n")
	out := make([]string, 0, len(lines)+2)
	warnings := make([]string, 0, 2)

	inInterface := false
	seenTable := false
	warningSeen := map[string]struct{}{}

	injectTableIfNeeded := func() {
		if inInterface && !seenTable {
			out = append(out, fmt.Sprintf("Table = %d", routeTable))
			seenTable = true
		}
	}

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			injectTableIfNeeded()
			section := strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			inInterface = section == "interface"
			if inInterface {
				seenTable = false
			}
			out = append(out, line)
			continue
		}
		if inInterface {
			key, value, ok := splitINIKeyValue(trimmed)
			if ok {
				lowerKey := strings.ToLower(strings.TrimSpace(key))
				if lowerKey == "table" {
					seenTable = true
				}
				if lowerKey == "dns" && !dnsSupported {
					if _, exists := warningSeen["dns"]; !exists {
						warnings = append(warnings, "Removed WireGuard DNS directive because resolvconf is unavailable on this system")
						warningSeen["dns"] = struct{}{}
					}
					continue
				}
				if (lowerKey == "postup" || lowerKey == "predown" || lowerKey == "postdown") && containsLegacyUpDownScript(value) {
					if _, exists := warningSeen[lowerKey]; !exists {
						warnings = append(warnings, fmt.Sprintf("Removed legacy peacey/split-vpn %s hook from WireGuard config", key))
						warningSeen[lowerKey] = struct{}{}
					}
					continue
				}
			}
		}
		out = append(out, line)
	}
	injectTableIfNeeded()

	joined := strings.Join(out, "\n")
	if !strings.HasSuffix(joined, "\n") {
		joined += "\n"
	}
	if err := ValidateWGConfig(joined); err != nil {
		return "", nil, err
	}
	return joined, warnings, nil
}

func containsLegacyUpDownScript(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "split-vpn/vpn/updown.sh")
}

func hasResolvconfBinary() bool {
	resolvconfCheckOnce.Do(func() {
		_, err := exec.LookPath("resolvconf")
		resolvconfAvailable = err == nil
	})
	return resolvconfAvailable
}
