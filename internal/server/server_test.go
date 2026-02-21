package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"split-vpn-webui/internal/routing"
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
	request := requestWithVPNNameParam("sgp.swic.name")

	name, ok := s.requireVPNNameParam(recorder, request)
	if !ok {
		t.Fatalf("expected valid name to pass validation")
	}
	if name != "sgp.swic.name" {
		t.Fatalf("expected name to round-trip, got %q", name)
	}
}

func TestDecodeGroupPayloadRejectsInvalidDomain(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/groups", strings.NewReader(`{"name":"Gaming","egressVpn":"sgp.swic.name","domains":["bad domain"]}`))

	_, err := decodeGroupPayload(request)
	if err == nil {
		t.Fatalf("expected invalid domain to fail")
	}
	if !errors.Is(err, routing.ErrGroupValidation) {
		t.Fatalf("expected ErrGroupValidation, got %v", err)
	}
}

func TestDecodeGroupPayloadNormalizesDomains(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/groups", strings.NewReader(`{"name":"Gaming","egressVpn":"sgp.swic.name","domains":["*.Example.com","example.com"]}`))

	group, err := decodeGroupPayload(request)
	if err != nil {
		t.Fatalf("expected valid payload, got %v", err)
	}
	if len(group.Domains) != 1 || group.Domains[0] != "example.com" {
		t.Fatalf("expected normalized domains [example.com], got %#v", group.Domains)
	}
}

func requestWithVPNNameParam(name string) *http.Request {
	request := httptest.NewRequest("GET", "/api/vpns/"+name, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", name)
	return request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, rctx))
}
