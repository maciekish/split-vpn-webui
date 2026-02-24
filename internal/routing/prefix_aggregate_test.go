package routing

import (
	"reflect"
	"testing"
)

func TestCollapseSetEntriesIPv4Hosts(t *testing.T) {
	out, err := collapseSetEntries([]string{
		"198.51.100.0/32",
		"198.51.100.1/32",
		"198.51.100.2/32",
		"198.51.100.3/32",
	}, "inet")
	if err != nil {
		t.Fatalf("collapseSetEntries failed: %v", err)
	}
	want := []string{"198.51.100.0/30"}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("expected %v, got %v", want, out)
	}
}

func TestCollapseSetEntriesIPv6Hosts(t *testing.T) {
	out, err := collapseSetEntries([]string{
		"2001:db8::1/128",
		"2001:db8::2/128",
		"2001:db8::3/128",
		"2001:db8::4/128",
	}, "inet6")
	if err != nil {
		t.Fatalf("collapseSetEntries failed: %v", err)
	}
	want := []string{"2001:db8::1/128", "2001:db8::2/127", "2001:db8::4/128"}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("expected %v, got %v", want, out)
	}
}

func TestCollapseSetEntriesRemovesDuplicatesAndMixesHostForms(t *testing.T) {
	out, err := collapseSetEntries([]string{
		"203.0.113.10",
		"203.0.113.10/32",
		"203.0.113.11",
		"203.0.113.11/32",
	}, "inet")
	if err != nil {
		t.Fatalf("collapseSetEntries failed: %v", err)
	}
	want := []string{"203.0.113.10/31"}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("expected %v, got %v", want, out)
	}
}

func TestCollapseSetEntriesRejectsFamilyMismatch(t *testing.T) {
	if _, err := collapseSetEntries([]string{"203.0.113.10/32"}, "inet6"); err == nil {
		t.Fatalf("expected family mismatch error")
	}
}
