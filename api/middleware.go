package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/rbac"
)

// maxRequestBodyBytes caps every request body so a malicious or buggy client
// can't exhaust memory/disk by streaming an arbitrarily large payload (e.g.
// into the text editor's save endpoint). 2 MiB comfortably covers a large
// pasted text while staying far below anything that could hurt the server.
const maxRequestBodyBytes = 2 << 20 // 2 MiB

func withBodyLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
		next.ServeHTTP(w, r)
	})
}

type contextKey string

const claimsContextKey contextKey = "claims"

// publicAPIPaths lists /api routes reachable without a token — signing up and
// logging in must stay open since you don't have one yet at that point.
var publicAPIPaths = map[string]bool{
	"/api/auth/register": true,
	"/api/auth/login":    true,
	"/api/auth/confirm":  true,
	"/api/auth/google":   true,
}

// requireToken verifies the bearer JWT on every /api request other than
// publicAPIPaths, and attaches its rbac.Claims to the request context for
// handlers to read (see claimsFromContext) — both to identify the caller
// (Claims.Subject, the api user ID) and to authorize gated actions
// (Claims.Allows).
func requireToken(jwtSecret []byte, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api") || publicAPIPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		token := bearerToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "token manquant")
			return
		}

		claims, err := rbac.VerifyToken(token, jwtSecret)
		if err != nil {
			if errors.Is(err, rbac.ErrExpiredToken) {
				writeError(w, http.StatusUnauthorized, "session expirée")
				return
			}
			writeError(w, http.StatusUnauthorized, "token invalide ou expiré")
			return
		}

		ctx := context.WithValue(r.Context(), claimsContextKey, claims)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// claimsFromContext fetches the rbac.Claims requireToken attached to r's
// context. ok is false only if this handler is reachable without going
// through requireToken first — a wiring bug, not a runtime condition to
// recover from gracefully.
func claimsFromContext(r *http.Request) (rbac.Claims, bool) {
	claims, ok := r.Context().Value(claimsContextKey).(rbac.Claims)
	return claims, ok
}

// requirePermission checks the caller's claims against action, writing the
// appropriate error response and returning false if the handler should
// stop rather than proceed.
func requirePermission(w http.ResponseWriter, r *http.Request, action rbac.Action) bool {
	claims, ok := claimsFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "token manquant")
		return false
	}
	if !claims.Allows(action) {
		writeError(w, http.StatusForbidden, "droits insuffisants")
		return false
	}
	return true
}

// requireSubscription writes a 403 and returns false unless the caller
// follows textID — the api-side half of the rule the front ends show: a
// text is acted on by the people who chose to follow it.
//
// Deliberately *on top of* requirePermission rather than instead of it.
// The two answer different questions: the backoffice decides what an
// account may ever do, following decides which texts it does it to. An
// account without the permission is refused whatever it follows, and one
// with it is still refused on a text it has not taken up.
//
// Root passes without following anything. It bypasses every rbac check
// already (see rbac.Claims.Allows), and an administrator made to subscribe
// to a text before removing it would be signing up for its notifications
// just to moderate it.
//
// Voting is deliberately not gated this way: settling a round somebody
// else opened is exactly what a passer-by should be able to do.
func requireSubscription(w http.ResponseWriter, r *http.Request, repo *queel.Repository, textID string) bool {
	claims, ok := claimsFromContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "token manquant")
		return false
	}
	if claims.Root {
		return true
	}

	subscribed, err := repo.IsSubscribed(claims.Subject, textID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erreur serveur")
		return false
	}
	if !subscribed {
		writeError(w, http.StatusForbidden, "abonnez-vous à ce texte pour agir dessus")
		return false
	}
	return true
}

func bearerToken(r *http.Request) string {
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}
