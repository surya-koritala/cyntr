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
