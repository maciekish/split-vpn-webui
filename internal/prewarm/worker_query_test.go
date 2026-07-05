package prewarm

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"split-vpn-webui/internal/routing"
	"split-vpn-webui/internal/vpn"
)

func TestResilientQueryRetriesUntilSuccess(t *testing.T) {
	w := &Worker{attempts: 3, timeout: time.Second}
	var calls int
	got, err := w.resilientQuery(context.Background(), func(ctx context.Context) ([]string, error) {
		calls++
		if calls < 3 {
			return nil, fmt.Errorf("transient failure %d", calls)
		}
		return []string{"1.2.3.4"}, nil
	})
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if calls != 3 {
		t.Fatalf("expected 3 attempts, got %d", calls)
	}
	if strings.Join(got, ",") != "1.2.3.4" {
		t.Fatalf("unexpected result: %#v", got)
	}
}

func TestResilientQueryGivesUpAfterAttempts(t *testing.T) {
	w := &Worker{attempts: 3, timeout: time.Second}
	var calls int
	_, err := w.resilientQuery(context.Background(), func(ctx context.Context) ([]string, error) {
		calls++
		return nil, fmt.Errorf("permanent failure")
	})
	if err == nil {
		t.Fatalf("expected error after exhausting attempts")
	}
	if calls != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", calls)
	}
}

func TestResilientQueryEnforcesPerAttemptTimeout(t *testing.T) {
	w := &Worker{attempts: 2, timeout: 20 * time.Millisecond}
	var calls int
	start := time.Now()
	_, err := w.resilientQuery(context.Background(), func(ctx context.Context) ([]string, error) {
		calls++
		<-ctx.Done()
		return nil, ctx.Err()
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatalf("expected timeout error")
	}
	if calls != 2 {
		t.Fatalf("expected 2 bounded attempts, got %d", calls)
	}
	if elapsed > time.Second {
		t.Fatalf("expected bounded attempts to finish quickly, took %s", elapsed)
	}
}

func TestResilientQueryAbortsOnParentCancel(t *testing.T) {
	w := &Worker{attempts: 3, timeout: time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls int
	_, err := w.resilientQuery(ctx, func(ctx context.Context) ([]string, error) {
		calls++
		return nil, fmt.Errorf("should not run")
	})
	if err == nil {
		t.Fatalf("expected cancellation error")
	}
	if calls != 0 {
		t.Fatalf("expected no attempts after parent cancel, got %d", calls)
	}
}

type flakyDoH struct {
	mu        sync.Mutex
	failFirst map[string]int
	data      map[string][]string
	calls     map[string]int
}

func (f *flakyDoH) QueryA(ctx context.Context, domain, iface string) ([]string, error) {
	return f.query("A", domain, iface)
}

func (f *flakyDoH) QueryAAAA(ctx context.Context, domain, iface string) ([]string, error) {
	return f.query("AAAA", domain, iface)
}

func (f *flakyDoH) QueryCNAME(ctx context.Context, domain, iface string) ([]string, error) {
	return f.query("CNAME", domain, iface)
}

func (f *flakyDoH) query(qType, domain, iface string) ([]string, error) {
	key := fmt.Sprintf("%s|%s|%s", strings.ToLower(iface), strings.ToLower(domain), qType)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.calls == nil {
		f.calls = map[string]int{}
	}
	f.calls[key]++
	if f.failFirst[key] > 0 {
		f.failFirst[key]--
		return nil, fmt.Errorf("transient failure for %s", key)
	}
	return append([]string(nil), f.data[key]...), nil
}

func TestWorkerRecoversFromTransientResolverFailures(t *testing.T) {
	groups := &mockGroupSource{
		groups: []routing.DomainGroup{
			{Name: "Flaky", EgressVPN: "wg-a", Domains: []string{"flaky.example"}},
		},
	}
	vpns := &mockVPNSource{
		profiles: []*vpn.VPNProfile{{Name: "wg-a", InterfaceName: "wg-a"}},
	}
	doh := &flakyDoH{
		failFirst: map[string]int{"wg-a|flaky.example|A": 2},
		data: map[string][]string{
			"wg-a|flaky.example|CNAME": {},
			"wg-a|flaky.example|A":     {"203.0.113.5"},
			"wg-a|flaky.example|AAAA":  {},
		},
	}
	ipset := &mockIPSet{}

	worker, err := NewWorker(groups, vpns, doh, ipset, WorkerOptions{
		Attempts:        3,
		Timeout:         time.Second,
		InterfaceActive: func(name string) (bool, error) { return true, nil },
	})
	if err != nil {
		t.Fatalf("NewWorker failed: %v", err)
	}

	stats, err := worker.Run(context.Background())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if stats.IPsInserted != 1 {
		t.Fatalf("expected transient failures to recover into 1 inserted IP, got %d", stats.IPsInserted)
	}
	v4Set, _ := routing.GroupSetNames("Flaky")
	if got := strings.Join(stats.CacheSnapshot[v4Set].V4, ","); got != "203.0.113.5" {
		t.Fatalf("unexpected v4 values after recovery: %q", got)
	}
	if doh.calls["wg-a|flaky.example|A"] != 3 {
		t.Fatalf("expected 3 A attempts (2 failures + 1 success), got %d", doh.calls["wg-a|flaky.example|A"])
	}
}
