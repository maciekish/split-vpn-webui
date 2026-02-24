package prewarm

import (
	"testing"
	"time"
)

func TestBuildQueryResolversKeepsCloudflareFirst(t *testing.T) {
	primary := &mockDoH{data: map[string][]string{}}
	additional := &mockDoH{data: map[string][]string{}}

	resolvers, err := buildQueryResolvers(primary, WorkerOptions{
		Timeout:             2 * time.Second,
		AdditionalResolvers: []DoHClient{additional},
		ExtraNameservers:    []string{"9.9.9.9", "9.9.9.9"},
		ECSProfiles:         []string{"31.13.64.0/18"},
	})
	if err != nil {
		t.Fatalf("buildQueryResolvers failed: %v", err)
	}
	if len(resolvers) != 4 {
		t.Fatalf("expected 4 resolvers, got %d", len(resolvers))
	}
	if resolvers[0] != primary {
		t.Fatalf("expected primary resolver first")
	}
	if resolvers[1] != additional {
		t.Fatalf("expected additional resolver second")
	}
	if _, ok := resolvers[2].(*NameserverClient); !ok {
		t.Fatalf("expected nameserver resolver third, got %T", resolvers[2])
	}
	ecsClient, ok := resolvers[3].(*CloudflareDoHClient)
	if !ok {
		t.Fatalf("expected ECS DoH resolver fourth, got %T", resolvers[3])
	}
	if ecsClient.extraQuery["edns_client_subnet"] != "31.13.64.0/18" {
		t.Fatalf("unexpected ECS subnet in DoH resolver: %#v", ecsClient.extraQuery)
	}
}

func TestBuildQueryResolversRejectsInvalidNameserver(t *testing.T) {
	primary := &mockDoH{data: map[string][]string{}}
	_, err := buildQueryResolvers(primary, WorkerOptions{
		ExtraNameservers: []string{"invalid"},
	})
	if err == nil {
		t.Fatalf("expected invalid nameserver error")
	}
}
