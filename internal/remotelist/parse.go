package remotelist

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"split-vpn-webui/internal/routing"
)

// parseBody turns a downloaded list body into canonical selector values for the
// given kind. Blank lines and `#`, `;` or `//` comments are ignored; each
// remaining line contributes its first whitespace-separated field.
func parseBody(kind string, body []byte) (entries []string, skipped int, err error) {
	raw := make([]string, 0, 256)
	scanner := bufio.NewScanner(bytes.NewReader(body))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		value := parseLine(scanner.Text())
		if value == "" {
			continue
		}
		raw = append(raw, value)
	}
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, 0, fmt.Errorf("read list body: %w", scanErr)
	}

	entries, skipped, err = routing.NormalizeRemoteListValues(kind, raw)
	if err != nil {
		return nil, 0, err
	}
	if limit := MaxEntriesForKind(kind); len(entries) > limit {
		return nil, 0, fmt.Errorf("list has %d entries, which exceeds the %d entry limit for %s lists", len(entries), limit, kind)
	}
	return entries, skipped, nil
}

func parseLine(line string) string {
	trimmed := strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
	trimmed = strings.TrimPrefix(trimmed, "\ufeff")
	if trimmed == "" {
		return ""
	}
	for _, marker := range []string{"#", ";", "//"} {
		if index := strings.Index(trimmed, marker); index >= 0 {
			trimmed = strings.TrimSpace(trimmed[:index])
		}
	}
	if trimmed == "" {
		return ""
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// hashEntries is the change-detection fingerprint for a parsed list. Entries are
// already sorted and de-duplicated, so an identical fingerprint means identical
// routing input even if the served file was reformatted.
func hashEntries(entries []string) string {
	digest := sha256.New()
	for _, entry := range entries {
		digest.Write([]byte(entry))
		digest.Write([]byte{'\n'})
	}
	return hex.EncodeToString(digest.Sum(nil))
}
