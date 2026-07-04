package vpn

import (
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{
		"wg-sgp",
		"a",
		"WG_01.test",
		strings.Repeat("a", 64),
	}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Fatalf("expected valid name %q, got error: %v", name, err)
		}
	}

	invalid := []string{
		"",
		" bad",
		"bad ",
		"bad name",
		"bad/name",
		`bad\\name`,
		"bad@name",
		"../bad",
		"bad..name",
		"-bad",
		".bad",
		strings.Repeat("a", 65),
	}
	for _, name := range invalid {
		if err := ValidateName(name); err == nil {
			t.Fatalf("expected invalid name %q to fail validation", name)
		}
	}
}

func TestValidateMSSClamp(t *testing.T) {
	valid := map[string]string{
		"":       "",
		"  ":     "",
		"pmtu":   "pmtu",
		"PMTU":   "pmtu",
		" 1340 ": "1340",
		"400":    "400",
		"1440":   "1440",
	}
	for input, want := range valid {
		got, err := ValidateMSSClamp(input)
		if err != nil {
			t.Fatalf("ValidateMSSClamp(%q) unexpected error: %v", input, err)
		}
		if got != want {
			t.Fatalf("ValidateMSSClamp(%q) = %q, want %q", input, got, want)
		}
	}

	invalid := []string{"399", "1441", "0", "-1", "abc", "12.5", "auto"}
	for _, input := range invalid {
		if _, err := ValidateMSSClamp(input); err == nil {
			t.Fatalf("expected ValidateMSSClamp(%q) to fail", input)
		}
	}
}

func TestValidateDomain(t *testing.T) {
	valid := []string{
		"example.com",
		"*.example.com",
		"sub.example.co.uk",
		"xn--d1acpjx3f.xn--p1ai",
	}
	for _, domain := range valid {
		if err := ValidateDomain(domain); err != nil {
			t.Fatalf("expected valid domain %q, got error: %v", domain, err)
		}
	}

	invalid := []string{
		"",
		"example",
		"exa mple.com",
		"*example.com",
		"example..com",
		"-bad.com",
		"bad-.com",
		"exa_mple.com",
		"example.com.",
	}
	for _, domain := range invalid {
		if err := ValidateDomain(domain); err == nil {
			t.Fatalf("expected invalid domain %q to fail validation", domain)
		}
	}
}
