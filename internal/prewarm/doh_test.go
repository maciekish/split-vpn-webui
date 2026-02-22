package prewarm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCloudflareDoHClientParsesAAndAAAAAndCNAME(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		qType := strings.ToUpper(r.URL.Query().Get("type"))
		name := strings.ToLower(strings.TrimSuffix(r.URL.Query().Get("name"), "."))
		response := map[string]any{"Status": 0, "Answer": []map[string]any{}}
		switch {
		case qType == "A" && name == "max.com":
			response["Answer"] = []map[string]any{
				{"type": 1, "data": "1.1.1.1"},
				{"type": 1, "data": "1.1.1.2"},
				{"type": 28, "data": "2001:db8::1"},
				{"type": 1, "data": "not-an-ip"},
			}
		case qType == "AAAA" && name == "max.com":
			response["Answer"] = []map[string]any{
				{"type": 28, "data": "2001:db8::1"},
				{"type": 28, "data": "2001:db8::2"},
				{"type": 1, "data": "1.1.1.1"},
			}
		case qType == "CNAME" && name == "max.com":
			response["Answer"] = []map[string]any{
				{"type": 5, "data": "Edge.Max.Com."},
				{"type": 5, "data": "edge.max.com."},
			}
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewCloudflareDoHClientWithURL(server.URL, 2*time.Second)
	ctx := context.Background()

	v4, err := client.QueryA(ctx, "max.com", "")
	if err != nil {
		t.Fatalf("QueryA failed: %v", err)
	}
	if len(v4) != 2 || v4[0] != "1.1.1.1" || v4[1] != "1.1.1.2" {
		t.Fatalf("unexpected A records: %#v", v4)
	}

	v6, err := client.QueryAAAA(ctx, "max.com", "")
	if err != nil {
		t.Fatalf("QueryAAAA failed: %v", err)
	}
	if len(v6) != 2 || v6[0] != "2001:db8::1" || v6[1] != "2001:db8::2" {
		t.Fatalf("unexpected AAAA records: %#v", v6)
	}

	cnames, err := client.QueryCNAME(ctx, "max.com", "")
	if err != nil {
		t.Fatalf("QueryCNAME failed: %v", err)
	}
	if len(cnames) != 1 || cnames[0] != "edge.max.com" {
		t.Fatalf("unexpected CNAME records: %#v", cnames)
	}
}

func TestCloudflareDoHClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_ = json.NewEncoder(w).Encode(map[string]any{"Status": 0, "Answer": []map[string]any{}})
	}))
	defer server.Close()

	client := NewCloudflareDoHClientWithURL(server.URL, 10*time.Millisecond)
	_, err := client.QueryA(context.Background(), "max.com", "")
	if err == nil {
		t.Fatalf("expected timeout error")
	}
}
