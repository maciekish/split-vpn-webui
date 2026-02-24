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

func TestParseIPSetSnapshotsIncludesMembers(t *testing.T) {
	raw := `
Name: svpn_media_r1d4
Type: hash:net
Number of entries: 2
Members:
1.1.1.1 timeout 86399
104.16.0.0/12 timeout 86399

Name: svpn_media_r1d6
Type: hash:net
Number of entries: 1
Members:
2606:4700::/32 timeout 86399
`
	snapshots, err := parseIPSetSnapshots(raw)
	if err != nil {
		t.Fatalf("parseIPSetSnapshots failed: %v", err)
	}
	if snapshots["svpn_media_r1d4"].Count != 2 {
		t.Fatalf("expected v4 count 2, got %d", snapshots["svpn_media_r1d4"].Count)
	}
	if len(snapshots["svpn_media_r1d4"].Members) != 2 {
		t.Fatalf("expected two v4 members, got %d", len(snapshots["svpn_media_r1d4"].Members))
	}
	if snapshots["svpn_media_r1d4"].Members[0] != "1.1.1.1" {
		t.Fatalf("unexpected first v4 member: %q", snapshots["svpn_media_r1d4"].Members[0])
	}
	if snapshots["svpn_media_r1d6"].Members[0] != "2606:4700::/32" {
		t.Fatalf("unexpected first v6 member: %q", snapshots["svpn_media_r1d6"].Members[0])
	}
}
