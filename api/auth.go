package main

import (
	"crypto/rand"
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
	Token     string `json:"token"`
	ExpiresAt string `json:"expiresAt"`
	UserID    string `json:"userId"`
	Email     string `json:"email"`
	Pseudo    string `json:"pseudo,omitempty"`
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

		token, expiresAt, err := issueToken(rbacStore, jwtSecret, user)
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
			Token:     token,
			ExpiresAt: expiresAt.Format(time.RFC3339),
			UserID:    user.ID,
			Email:     user.Email,
			Pseudo:    pseudo,
		})
	}
}

// issueToken builds a JWT for user: its subject is the api's own user ID
// (what the rest of this codebase already uses as an author/voter ID), and
// its permission claims come from the rbac directory entry user.RbacUUID
// points at, if any has been assigned yet. A user with no rbac UUID still
// gets a valid token — they just can't do anything gated by a permission
// until an admin assigns them one (see admin.go).
func issueToken(rbacStore *rbac.Store, jwtSecret []byte, user *User) (token string, expiresAt time.Time, err error) {
	var perms rbac.Permissions
	var root bool
	if user.RbacUUID != nil {
		rbacUser, err := rbacStore.GetUser(*user.RbacUUID)
		if err != nil && !errors.Is(err, rbac.ErrNotFound) {
			return "", time.Time{}, err
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
		return "", time.Time{}, err
	}
	return token, expiresAt, nil
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
