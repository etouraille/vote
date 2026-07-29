package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A custom request header only reaches the handler if the preflight says
// it's allowed, so the mobile client header has to be advertised here —
// otherwise the browser drops it and googleAudience never sees it.
func TestCORSAllowsTheClientHeader(t *testing.T) {
	rec := httptest.NewRecorder()
	withCORS(http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/api/auth/google", nil))

	allowed := rec.Header().Get("Access-Control-Allow-Headers")
	for _, header := range []string{"Content-Type", "Authorization", clientHeader} {
		if !strings.Contains(allowed, header) {
			t.Errorf("Access-Control-Allow-Headers %q is missing %q", allowed, header)
		}
	}
}

// Preflights must be answered by the middleware itself rather than handed
// down the chain, since they carry no Authorization header.
func TestCORSAnswersPreflightWithoutCallingTheHandler(t *testing.T) {
	called := false
	handler := withCORS(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodOptions, "/api/auth/google", nil))

	if called {
		t.Error("preflight reached the wrapped handler")
	}
	if rec.Code != http.StatusNoContent {
		t.Errorf("got status %d, want %d", rec.Code, http.StatusNoContent)
	}
}
