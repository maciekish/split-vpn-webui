package update

import "testing"

func TestIsNewerVersionSemver(t *testing.T) {
	cases := []struct {
		current   string
		candidate string
		want      bool
	}{
		{current: "v1.0.0", candidate: "v1.0.1", want: true},
		{current: "v1.9.0", candidate: "v2.0.0", want: true},
		{current: "v1.2.3", candidate: "v1.2.3", want: false},
		{current: "v1.2.3", candidate: "v1.1.9", want: false},
		{current: "dev", candidate: "v1.0.0", want: true},
		{current: "v1.0.0", candidate: "dev", want: true},
	}
	for _, tc := range cases {
		got := isNewerVersion(tc.current, tc.candidate)
		if got != tc.want {
			t.Fatalf("isNewerVersion(%q, %q) = %v, want %v", tc.current, tc.candidate, got, tc.want)
		}
	}
}

func TestNormalizeTag(t *testing.T) {
	if _, err := normalizeTag(""); err == nil {
		t.Fatalf("expected empty tag error")
	}
	if _, err := normalizeTag("../bad"); err == nil {
		t.Fatalf("expected invalid character error")
	}
	tag, err := normalizeTag("v1.2.3")
	if err != nil {
		t.Fatalf("normalizeTag failed: %v", err)
	}
	if tag != "v1.2.3" {
		t.Fatalf("unexpected normalized tag: %q", tag)
	}
}
