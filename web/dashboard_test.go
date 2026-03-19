package web

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashboardServesHTML(t *testing.T) {
	handler := NewDashboardHandler()

	// FileServer redirects /index.html -> /; request root directly
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	body := w.Body.String()
	if !strings.Contains(body, "Cyntr Dashboard") {
		t.Fatal("expected dashboard HTML")
	}
}

func TestDashboardRootRedirect(t *testing.T) {
	handler := NewDashboardHandler()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// FileServer serves index.html for /
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestDashboardHasSections(t *testing.T) {
	handler := NewDashboardHandler()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()

	sections := []string{
		"page-dashboard",
		"page-agents",
		"page-audit",
		"page-policies",
		"page-federation",
	}
	for _, s := range sections {
		if !strings.Contains(body, s) {
			t.Errorf("expected section %q in dashboard HTML", s)
		}
	}
}

func TestDashboardHasNavItems(t *testing.T) {
	handler := NewDashboardHandler()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()

	navItems := []string{"Dashboard", "Agents", "Audit", "Policies", "Federation"}
	for _, item := range navItems {
		if !strings.Contains(body, item) {
			t.Errorf("expected nav item %q in dashboard HTML", item)
		}
	}
}

func TestDashboardHasAPIEndpoints(t *testing.T) {
	handler := NewDashboardHandler()

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	body := w.Body.String()

	endpoints := []string{
		"/api/v1/system/health",
		"/api/v1/system/version",
		"/api/v1/tenants",
		"/api/v1/audit",
		"/api/v1/policies/test",
		"/api/v1/federation/peers",
	}
	for _, ep := range endpoints {
		if !strings.Contains(body, ep) {
			t.Errorf("expected API endpoint %q referenced in dashboard HTML", ep)
		}
	}
}
