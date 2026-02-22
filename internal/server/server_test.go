package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"split-vpn-webui/internal/config"
	"split-vpn-webui/internal/routing"
	"split-vpn-webui/internal/systemd"
)

func TestRequireVPNNameParamRejectsTraversal(t *testing.T) {
	s := &Server{}
	recorder := httptest.NewRecorder()
	request := requestWithVPNNameParam("../etc/passwd")

	_, ok := s.requireVPNNameParam(recorder, request)
	if ok {
		t.Fatalf("expected traversal name to be rejected")
	}
	if recorder.Code != 400 {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "invalid vpn name") {
		t.Fatalf("expected invalid vpn name error, got %q", recorder.Body.String())
	}
}

func TestRequireVPNNameParamRejectsOverlongName(t *testing.T) {
	s := &Server{}
	recorder := httptest.NewRecorder()
	request := requestWithVPNNameParam(strings.Repeat("a", 65))

	_, ok := s.requireVPNNameParam(recorder, request)
	if ok {
		t.Fatalf("expected overlong name to be rejected")
	}
	if recorder.Code != 400 {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestRequireVPNNameParamAcceptsValidName(t *testing.T) {
	s := &Server{}
	recorder := httptest.NewRecorder()
	request := requestWithVPNNameParam("sgp.contoso.com")

	name, ok := s.requireVPNNameParam(recorder, request)
	if !ok {
		t.Fatalf("expected valid name to pass validation")
	}
	if name != "sgp.contoso.com" {
		t.Fatalf("expected name to round-trip, got %q", name)
	}
}

func TestDecodeGroupPayloadRejectsInvalidDomain(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/groups", strings.NewReader(`{"name":"Gaming","egressVpn":"sgp.contoso.com","domains":["bad domain"]}`))

	_, err := decodeGroupPayload(request)
	if err == nil {
		t.Fatalf("expected invalid domain to fail")
	}
	if !errors.Is(err, routing.ErrGroupValidation) {
		t.Fatalf("expected ErrGroupValidation, got %v", err)
	}
}

func TestDecodeGroupPayloadNormalizesDomains(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/groups", strings.NewReader(`{"name":"Gaming","egressVpn":"sgp.contoso.com","domains":["*.Example.com","example.com"]}`))

	group, err := decodeGroupPayload(request)
	if err != nil {
		t.Fatalf("expected valid payload, got %v", err)
	}
	if len(group.Domains) != 1 || group.Domains[0] != "example.com" {
		t.Fatalf("expected normalized domains [example.com], got %#v", group.Domains)
	}
}

func TestDecodeGroupPayloadParsesSourceInterfaceAndMACSelectors(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/groups", strings.NewReader(`{
		"name":"LAN",
		"egressVpn":"sgp.contoso.com",
		"rules":[
			{
				"name":"Device",
				"sourceInterfaces":["BR6"],
				"sourceMacs":["00:30:93:10:0A:12"],
				"destinationPorts":[{"protocol":"both","start":53}]
			}
		]
	}`))

	group, err := decodeGroupPayload(request)
	if err != nil {
		t.Fatalf("expected valid payload, got %v", err)
	}
	if len(group.Rules) != 1 {
		t.Fatalf("expected one rule, got %d", len(group.Rules))
	}
	rule := group.Rules[0]
	if len(rule.SourceInterfaces) != 1 || rule.SourceInterfaces[0] != "br6" {
		t.Fatalf("unexpected source interfaces: %#v", rule.SourceInterfaces)
	}
	if len(rule.SourceMACs) != 1 || rule.SourceMACs[0] != "00:30:93:10:0a:12" {
		t.Fatalf("unexpected source MACs: %#v", rule.SourceMACs)
	}
	if len(rule.DestinationPorts) != 1 || rule.DestinationPorts[0].Protocol != "both" {
		t.Fatalf("unexpected destination ports: %#v", rule.DestinationPorts)
	}
}

func requestWithVPNNameParam(name string) *http.Request {
	request := httptest.NewRequest("GET", "/api/vpns/"+name, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, rctx))
}

func TestHandleAutostartDoesNotCallSystemdEnableDisable(t *testing.T) {
	base := t.TempDir()
	vpnDir := filepath.Join(base, "Test")
	if err := os.MkdirAll(vpnDir, 0o700); err != nil {
		t.Fatalf("mkdir vpn dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(vpnDir, "vpn.conf"), []byte("DEV=wg-sv-test\n"), 0o644); err != nil {
		t.Fatalf("write vpn.conf: %v", err)
	}

	cm := config.NewManager(base)
	if _, err := cm.Discover(); err != nil {
		t.Fatalf("discover configs: %v", err)
	}
	mockSystemd := &systemd.MockManager{
		EnableFunc: func(string) error {
			t.Fatalf("Enable should not be called by autostart toggle")
			return nil
		},
		DisableFunc: func(string) error {
			t.Fatalf("Disable should not be called by autostart toggle")
			return nil
		},
	}
	s := &Server{configManager: cm, systemd: mockSystemd}

	body := strings.NewReader(`{"enabled":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/configs/Test/autostart", body)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "Test")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	rec := httptest.NewRecorder()
	s.handleAutostart(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("expected status ok, got %#v", payload)
	}
	enabled, err := cm.AutostartEnabled("Test")
	if err != nil {
		t.Fatalf("autostart enabled check failed: %v", err)
	}
	if !enabled {
		t.Fatalf("expected autostart marker to be enabled")
	}
}

func TestDecodeUpdateTag(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/update/check", strings.NewReader(`{"version":"v1.2.3"}`))
	tag, err := decodeUpdateTag(req)
	if err != nil {
		t.Fatalf("decodeUpdateTag failed: %v", err)
	}
	if tag != "v1.2.3" {
		t.Fatalf("unexpected tag: %q", tag)
	}
}

func TestDecodeUpdateTagEmptyBody(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/update/check", http.NoBody)
	req.Body = io.NopCloser(strings.NewReader(""))
	tag, err := decodeUpdateTag(req)
	if err != nil {
		t.Fatalf("expected empty body to pass, got %v", err)
	}
	if tag != "" {
		t.Fatalf("expected empty tag, got %q", tag)
	}
}

func TestHandleUpdateStatusWithoutUpdater(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/update/status", nil)
	rec := httptest.NewRecorder()
	s.handleUpdateStatus(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}
