package server

import "testing"

func TestParseIPSetSizes(t *testing.T) {
	raw := `
Name: svpn_group_r1d4
Type: hash:net
Revision: 7
Number of entries: 12
Members:

Name: svpn_group_r1d6
Type: hash:net
Revision: 7
Number of entries: 3
Members:
`
	sizes, err := parseIPSetSizes(raw)
	if err != nil {
		t.Fatalf("parseIPSetSizes failed: %v", err)
	}
	if sizes["svpn_group_r1d4"] != 12 {
		t.Fatalf("expected v4 size 12, got %d", sizes["svpn_group_r1d4"])
	}
	if sizes["svpn_group_r1d6"] != 3 {
		t.Fatalf("expected v6 size 3, got %d", sizes["svpn_group_r1d6"])
	}
}

func TestParseIPSetSizesRejectsInvalidCount(t *testing.T) {
	raw := `
Name: svpn_bad
Number of entries: nope
`
	if _, err := parseIPSetSizes(raw); err == nil {
		t.Fatalf("expected invalid count error")
	}
}
