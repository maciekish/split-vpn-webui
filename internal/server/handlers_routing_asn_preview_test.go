package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"split-vpn-webui/internal/routing"
)

func TestHandleASNPreviewInvalidJSON(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/routing/asn-preview", strings.NewReader("{bad"))
	rec := httptest.NewRecorder()
	s.handleASNPreview(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleASNPreviewRequiresASN(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/routing/asn-preview", strings.NewReader(`{"asns":["", "#comment"]}`))
	rec := httptest.NewRecorder()
	s.handleASNPreview(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestHandleASNPreviewUsesSanitizedSelectors(t *testing.T) {
	original := previewASNEntries
	defer func() { previewASNEntries = original }()

	var capturedASNs []string
	var capturedTimeout time.Duration
	previewASNEntries = func(ctx context.Context, asns []string, timeout time.Duration) (routing.ASNPreviewResult, error) {
		capturedASNs = append([]string(nil), asns...)
		capturedTimeout = timeout
		return routing.ASNPreviewResult{
			Items: []routing.ASNPreviewItem{
				{ASN: "AS15169", PrefixesV4: 10, EntriesV4: 8},
			},
			TotalEntriesV4: 8,
		}, nil
	}

	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/routing/asn-preview", strings.NewReader(`{
		"asns":[" AS15169 ","13335 #cloudflare","","#AS714"]
	}`))
	rec := httptest.NewRecorder()
	s.handleASNPreview(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Join(capturedASNs, ",") != "AS15169,13335" {
		t.Fatalf("unexpected preview ASN inputs: %#v", capturedASNs)
	}
	if capturedTimeout != 10*time.Second {
		t.Fatalf("unexpected preview timeout: %s", capturedTimeout)
	}

	var payload struct {
		Result routing.ASNPreviewResult `json:"result"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Result.TotalEntriesV4 != 8 {
		t.Fatalf("unexpected preview result: %#v", payload.Result)
	}
}

func TestHandleASNPreviewValidationError(t *testing.T) {
	original := previewASNEntries
	defer func() { previewASNEntries = original }()
	previewASNEntries = func(ctx context.Context, asns []string, timeout time.Duration) (routing.ASNPreviewResult, error) {
		return routing.ASNPreviewResult{}, errors.New("invalid ASN \"ASBAD\"")
	}

	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/routing/asn-preview", strings.NewReader(`{"asns":["ASBAD"]}`))
	rec := httptest.NewRecorder()
	s.handleASNPreview(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}
