package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	webID    = "web-client-id.apps.googleusercontent.com"
	mobileID = "mobile-client-id.apps.googleusercontent.com"
)

func requestWithClientHeader(t *testing.T, value string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/api/auth/google", nil)
	if value != "" {
		r.Header.Set(clientHeader, value)
	}
	return r
}

// The Angular front sends no client header at all, and must keep getting
// the web audience — the whole point of defaulting rather than requiring
// every caller to declare itself.
func TestGoogleAudienceDefaultsToWebClient(t *testing.T) {
	clientID, ok := googleAudience(requestWithClientHeader(t, ""), webID, mobileID)
	if !ok {
		t.Fatal("expected ok for a request with no client header")
	}
	if clientID != webID {
		t.Fatalf("got %q, want the web client %q", clientID, webID)
	}
}

func TestGoogleAudienceUsesMobileClientForTheApp(t *testing.T) {
	clientID, ok := googleAudience(requestWithClientHeader(t, mobileClient), webID, mobileID)
	if !ok {
		t.Fatal("expected ok when a mobile client ID is configured")
	}
	if clientID != mobileID {
		t.Fatalf("got %q, want the mobile client %q", clientID, mobileID)
	}
}

// HTTP header values aren't case-sensitive by convention here, and a stray
// space is the kind of thing a hand-written client config grows — neither
// should silently drop the caller back onto the web audience, which would
// surface only as an unexplainable 401.
func TestGoogleAudienceAcceptsMobileCaseAndSpacingVariants(t *testing.T) {
	for _, value := range []string{"Mobile", "MOBILE", "  mobile  "} {
		clientID, ok := googleAudience(requestWithClientHeader(t, value), webID, mobileID)
		if !ok || clientID != mobileID {
			t.Errorf("header %q: got (%q, %v), want (%q, true)", value, clientID, ok, mobileID)
		}
	}
}

// An unknown client is not the mobile app, so it gets the web audience
// rather than an error — the header is an opt-in hint, not a whitelist.
func TestGoogleAudienceTreatsUnknownClientAsWeb(t *testing.T) {
	clientID, ok := googleAudience(requestWithClientHeader(t, "desktop"), webID, mobileID)
	if !ok {
		t.Fatal("expected ok for an unrecognized client")
	}
	if clientID != webID {
		t.Fatalf("got %q, want the web client %q", clientID, webID)
	}
}

// Failing loudly here is what keeps a missing GOOGLE_CLIENT_ID_MOBILE
// from being reported to users as an invalid Google token.
func TestGoogleAudienceRefusesMobileWhenUnconfigured(t *testing.T) {
	clientID, ok := googleAudience(requestWithClientHeader(t, mobileClient), webID, "")
	if ok {
		t.Fatalf("expected not ok with no mobile client configured, got %q", clientID)
	}
}

// Web sign-in must stay available on a server that never configured the
// mobile client — the two settings are independent.
func TestGoogleAudienceServesWebWhenMobileUnconfigured(t *testing.T) {
	clientID, ok := googleAudience(requestWithClientHeader(t, ""), webID, "")
	if !ok || clientID != webID {
		t.Fatalf("got (%q, %v), want (%q, true)", clientID, ok, webID)
	}
}
