package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/etouraille/queel/rbac"
	"golang.org/x/crypto/bcrypt"
)

const sessionTTL = time.Hour

// maxPseudoRunes bounds the optional display name set at registration —
// same length-cap-on-top-of-body-size-limit approach as text titles
// (see texts.go's maxTitleRunes).
const maxPseudoRunes = 50

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	// Pseudo is only meaningful on register; login ignores it if present.
	Pseudo string `json:"pseudo,omitempty"`
}

type sessionResponse struct {
	Token       string           `json:"token"`
	ExpiresAt   string           `json:"expiresAt"`
	UserID      string           `json:"userId"`
	Email       string           `json:"email"`
	Pseudo      string           `json:"pseudo,omitempty"`
	Root        bool             `json:"root"`
	Permissions rbac.Permissions `json:"permissions"`
}

type registerAck struct {
	Email   string `json:"email"`
	Message string `json:"message"`
}

func registerHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req credentialsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "corps de requête invalide")
			return
		}

		email := strings.ToLower(strings.TrimSpace(req.Email))
		if _, err := mail.ParseAddress(email); err != nil {
			writeError(w, http.StatusBadRequest, "email invalide")
			return
		}
		if len(req.Password) < 8 {
			writeError(w, http.StatusBadRequest, "le mot de passe doit contenir au moins 8 caractères")
			return
		}

		pseudo := strings.TrimSpace(req.Pseudo)
		if utf8.RuneCountInString(pseudo) > maxPseudoRunes {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("le pseudo ne doit pas dépasser %d caractères", maxPseudoRunes))
			return
		}

		hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		code := generateValidationCode()

		user, err := store.CreateUser(r.Context(), email, string(hash), code, pseudo)
		if err != nil {
			if errors.Is(err, ErrEmailTaken) {
				writeError(w, http.StatusConflict, "cet email est déjà utilisé")
				return
			}
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		if err := sendValidationEmail(user.Email, code); err != nil {
			log.Printf("failed to send validation email to %s: %v", user.Email, err)
			_ = store.DeleteUser(r.Context(), user.ID)
			writeError(w, http.StatusInternalServerError, "impossible d'envoyer l'email de confirmation, réessayez")
			return
		}

		writeJSON(w, http.StatusCreated, registerAck{
			Email:   user.Email,
			Message: "Compte créé. Vérifiez votre boîte mail pour valider votre inscription.",
		})
	}
}

func loginHandler(store *Store, rbacStore *rbac.Store, jwtSecret []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req credentialsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "corps de requête invalide")
			return
		}

		email := strings.ToLower(strings.TrimSpace(req.Email))
		user, err := store.UserByEmail(r.Context(), email)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "email ou mot de passe invalide")
			return
		}

		if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)) != nil {
			writeError(w, http.StatusUnauthorized, "email ou mot de passe invalide")
			return
		}

		if user.ValidationCode != nil {
			writeError(w, http.StatusForbidden, "compte non validé : vérifiez votre boîte mail pour confirmer votre inscription")
			return
		}

		token, expiresAt, perms, root, err := issueToken(rbacStore, jwtSecret, user)
		if err != nil {
			log.Printf("issuing token for %s: %v", user.Email, err)
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		var pseudo string
		if user.Pseudo != nil {
			pseudo = *user.Pseudo
		}
		writeJSON(w, http.StatusOK, sessionResponse{
			Token:       token,
			ExpiresAt:   expiresAt.Format(time.RFC3339),
			UserID:      user.ID,
			Email:       user.Email,
			Pseudo:      pseudo,
			Root:        root,
			Permissions: perms,
		})
	}
}

// issueToken builds a JWT for user: its subject is the api's own user ID
// (what the rest of this codebase already uses as an author/voter ID), and
// its permission claims come from the rbac directory entry user.RbacUUID
// points at, if any has been assigned yet. A user with no rbac UUID still
// gets a valid token — they just can't do anything gated by a permission
// until an admin assigns them one (see admin.go). Root/perms are also
// returned alongside the token so loginHandler can hand them straight to
// the front end without a follow-up /api/me round trip.
func issueToken(rbacStore *rbac.Store, jwtSecret []byte, user *User) (token string, expiresAt time.Time, perms rbac.Permissions, root bool, err error) {
	if user.RbacUUID != nil {
		rbacUser, err := rbacStore.GetUser(*user.RbacUUID)
		if err != nil && !errors.Is(err, rbac.ErrNotFound) {
			return "", time.Time{}, rbac.Permissions{}, false, err
		}
		if err == nil {
			perms = rbacUser.Permissions
			root = rbacUser.Root
		}
	}

	expiresAt = time.Now().Add(sessionTTL)
	claims := rbac.Claims{
		Subject:   user.ID,
		Root:      root,
		Perms:     perms.Bits(),
		ExpiresAt: expiresAt.Unix(),
	}
	token, err = rbac.SignToken(claims, jwtSecret)
	if err != nil {
		return "", time.Time{}, rbac.Permissions{}, false, err
	}
	return token, expiresAt, perms, root, nil
}

type googleAuthRequest struct {
	IDToken string `json:"idToken"`
	// Pseudo is required only the first time a given Google account signs
	// in (no existing user matches its email yet) — see
	// createUserFromGoogleSignIn.
	Pseudo string `json:"pseudo,omitempty"`
}

type needsPseudoResponse struct {
	NeedsPseudo bool `json:"needsPseudo"`
}

// clientHeader lets a caller say which of this project's front ends it is,
// so googleLoginHandler knows which OAuth client's audience to expect on an
// ID token. The Angular front sends nothing and gets the web client; the
// Flutter app sends mobileClient (see mobile's ApiClient) and gets the
// mobile one.
//
// Deliberately not a security boundary — anyone can set it. It only picks
// which audience is accepted, and both belong to the same Google project;
// the token itself is still verified against Google's signing keys either
// way, so claiming to be the app buys a caller nothing it couldn't already
// have by calling as the web front.
const clientHeader = "X-Queel-Client"

const mobileClient = "mobile"

// unverifiedGoogleClaims summarizes an ID token's own account of itself,
// for logging a rejection and nothing else — the signature is deliberately
// not checked here, so nothing it returns may be trusted or acted on.
//
// Only iss/aud/exp are pulled out: they are what a rejection turns on, and
// none of them is a secret (the audience is a public client ID, published
// in the front end's own source). The token itself is never logged.
func unverifiedGoogleClaims(idToken string) string {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return "unparseable (not a 3-part JWT)"
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "unparseable (bad base64 payload)"
	}
	var claims struct {
		Iss string `json:"iss"`
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "unparseable (bad JSON payload)"
	}
	return fmt.Sprintf("iss=%q aud=%q exp=%s", claims.Iss, claims.Aud, time.Unix(claims.Exp, 0).Format(time.RFC3339))
}

// googleAudience picks the OAuth client ID an incoming ID token must be
// audienced for, given who r claims to be.
//
// ok=false means the caller identified as the mobile app on a server that
// has no mobile client configured. That deliberately fails rather than
// falling back to webClientID: the fallback would let the request through
// to a `aud` check that cannot ever match, turning a missing setting into
// an "invalid Google token" 401 pointing nowhere near the actual cause.
func googleAudience(r *http.Request, webClientID, mobileClientID string) (clientID string, ok bool) {
	if !strings.EqualFold(strings.TrimSpace(r.Header.Get(clientHeader)), mobileClient) {
		return webClientID, true
	}
	if mobileClientID == "" {
		return "", false
	}
	return mobileClientID, true
}

// googleLoginHandler signs a caller in via "Sign in with Google": idToken
// is verified against Google's own signing keys (rbac.VerifyGoogleIDToken)
// rather than trusted at face value, so this never has to take the
// client's word for who they are — only Google's.
//
// An existing account (created here before, or through ordinary
// email+password registration sharing the same email) is just signed in —
// Google having verified that email is exactly the guarantee a clicked
// confirmation-email link gives register/confirmHandler. Whatever rbac
// permissions (or none, or root) that account already has are left
// completely alone: the only place this handler ever touches rbac is
// createUserFromGoogleSignIn below, which is only reached when no user
// matches the email at all — an existing account never passes through it,
// no matter what its current rights are.
//
// A first-time account (no user with this Google account's email yet)
// needs pseudo, which Google's identity doesn't supply; omitting it gets a
// needsPseudoResponse back instead of creating anything, so the front end
// can prompt for one and retry with the same idToken plus that pseudo.
//
// Web and mobile sign in through separate Google OAuth clients, so their
// tokens carry different `aud` claims and neither verifies against the
// other's client ID — googleAudience picks which one applies here.
func googleLoginHandler(store *Store, rbacStore *rbac.Store, jwtSecret []byte, googleClientID, mobileClientID string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req googleAuthRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "corps de requête invalide")
			return
		}

		clientID, ok := googleAudience(r, googleClientID, mobileClientID)
		if !ok {
			writeError(w, http.StatusServiceUnavailable, "connexion Google non configurée pour l'application mobile")
			return
		}

		identity, err := rbac.VerifyGoogleIDToken(req.IDToken, clientID)
		if err != nil {
			// rbac collapses issuer/audience/signature failures into one
			// error, so log what the token actually claims next to what was
			// expected — an `aud` mismatch is a configuration mistake and
			// looks identical, from the outside, to a forged token.
			log.Printf("google sign-in rejected: %v (expected aud %q, token claims %s)",
				err, clientID, unverifiedGoogleClaims(req.IDToken))
			if errors.Is(err, rbac.ErrGoogleTokenExpired) {
				writeError(w, http.StatusUnauthorized, "jeton Google expiré")
				return
			}
			writeError(w, http.StatusUnauthorized, "jeton Google invalide")
			return
		}
		if !identity.EmailVerified {
			writeError(w, http.StatusForbidden, "l'email de ce compte Google n'est pas vérifié")
			return
		}

		user, err := store.UserByEmail(r.Context(), identity.Email)
		if errors.Is(err, ErrUserNotFound) {
			created, ok := createUserFromGoogleSignIn(w, r, store, rbacStore, identity.Email, req.Pseudo)
			if !ok {
				return // createUserFromGoogleSignIn already wrote the response (error or needsPseudo)
			}
			user = created
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		token, expiresAt, perms, root, err := issueToken(rbacStore, jwtSecret, user)
		if err != nil {
			log.Printf("issuing token for %s via google: %v", user.Email, err)
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		var pseudo string
		if user.Pseudo != nil {
			pseudo = *user.Pseudo
		}
		writeJSON(w, http.StatusOK, sessionResponse{
			Token:       token,
			ExpiresAt:   expiresAt.Format(time.RFC3339),
			UserID:      user.ID,
			Email:       user.Email,
			Pseudo:      pseudo,
			Root:        root,
			Permissions: perms,
		})
	}
}

// createUserFromGoogleSignIn handles googleLoginHandler's "no existing user
// for this email" branch. A missing/blank pseudo writes needsPseudoResponse
// and reports ok=false rather than an error — the caller is expected to
// retry with the same idToken plus a pseudo once the front end has prompted
// for one — so ok=false does not always mean something went wrong.
//
// A newly created Google account gets CanVote-only rbac permissions
// immediately (unlike a fresh password registration, which starts with
// none until an admin assigns some — see admin.go), since there's no
// separate vetting step left to wait for once Google's already confirmed
// the email.
func createUserFromGoogleSignIn(w http.ResponseWriter, r *http.Request, store *Store, rbacStore *rbac.Store, email, rawPseudo string) (user *User, ok bool) {
	pseudo := strings.TrimSpace(rawPseudo)
	if pseudo == "" {
		writeJSON(w, http.StatusOK, needsPseudoResponse{NeedsPseudo: true})
		return nil, false
	}
	if utf8.RuneCountInString(pseudo) > maxPseudoRunes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("le pseudo ne doit pas dépasser %d caractères", maxPseudoRunes))
		return nil, false
	}

	unusablePassword, err := randomUnusablePasswordHash()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erreur serveur")
		return nil, false
	}

	created, err := store.CreateUserFromGoogle(r.Context(), email, unusablePassword, pseudo)
	if errors.Is(err, ErrEmailTaken) {
		// Lost a race with a concurrent Google sign-in for the same email
		// between UserByEmail and here — proceed with whichever account
		// won it, exactly as if it had been found the first time around.
		created, err = store.UserByEmail(r.Context(), email)
		if err == nil && created.RbacUUID != nil {
			// The winner's own flow already assigned rbac permissions —
			// nothing left to do (avoids creating a second, orphaned rbac
			// entry and overwriting theirs with it below).
			return created, true
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erreur serveur")
		return nil, false
	}

	rbacUser, err := rbacStore.CreateUser(false, rbac.Permissions{CanVote: true})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "erreur serveur")
		return nil, false
	}
	if err := store.SetRbacUUID(r.Context(), created.ID, rbacUser.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "erreur serveur")
		return nil, false
	}
	created.RbacUUID = &rbacUser.ID

	return created, true
}

// randomUnusablePasswordHash is stored in place of a real password for a
// Google-created account: nobody knows the random bytes it's derived from,
// so bcrypt.CompareHashAndPassword against it in loginHandler correctly
// always fails, rather than leaving the password column nullable.
func randomUnusablePasswordHash() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword(raw, bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

type confirmRequest struct {
	Email string `json:"email"`
	Code  int    `json:"code"`
}

func confirmHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req confirmRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "corps de requête invalide")
			return
		}

		email := strings.ToLower(strings.TrimSpace(req.Email))
		if err := store.ConfirmUser(r.Context(), email, req.Code); err != nil {
			writeError(w, http.StatusBadRequest, "code de validation invalide ou compte déjà validé")
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "compte validé, vous pouvez maintenant vous connecter"})
	}
}

func generateValidationCode() int {
	n, err := rand.Int(rand.Reader, big.NewInt(900000))
	if err != nil {
		panic(err)
	}
	return int(n.Int64()) + 100000
}
