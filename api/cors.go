package main

import "net/http"

// withCORS lets the Angular dev server (a different origin) call this API
// directly. It must wrap the token check so that preflight OPTIONS requests,
// which never carry an Authorization header, don't get rejected as unauthorized.
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		// clientHeader is custom, so a browser won't send it on a real
		// request unless the preflight lists it here — without that, the
		// Flutter web build's Google sign-in would silently fall back to
		// being treated as the Angular front (see googleAudience).
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, "+clientHeader)
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}
