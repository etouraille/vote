package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/etouraille/queel/rbac"
)

type permissionsRequest struct {
	Root             bool `json:"root"`
	CanVote          bool `json:"canVote"`
	CanCreateText    bool `json:"canCreateText"`
	CanCloseText     bool `json:"canCloseText"`
	CanSelect        bool `json:"canSelect"`
	CanEditSelection bool `json:"canEditSelection"`
	CanUpdateText    bool `json:"canUpdateText"`
}

// requireRoot writes a 403 and returns false unless the caller's claims
// (attached by requireToken) carry Root — the gate on every /api/admin/...
// route.
func requireRoot(w http.ResponseWriter, r *http.Request) bool {
	claims, ok := claimsFromContext(r)
	if !ok || !claims.Root {
		writeError(w, http.StatusForbidden, "réservé aux administrateurs")
		return false
	}
	return true
}

// adminUserResponse is one row of GET /api/admin/users: an api account
// alongside whatever rbac rights it currently has (all false/non-root if
// no rights have ever been assigned).
type adminUserResponse struct {
	ID          string           `json:"id"`
	Email       string           `json:"email"`
	Root        bool             `json:"root"`
	Permissions rbac.Permissions `json:"permissions"`
}

// listUsersHandler lists every api account for the admin backoffice to
// assign rights to, each merged with its current rbac permissions.
func listUsersHandler(store *Store, rbacStore *rbac.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireRoot(w, r) {
			return
		}

		users, err := store.ListUsers(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		result := make([]adminUserResponse, 0, len(users))
		for _, user := range users {
			resp := adminUserResponse{ID: user.ID, Email: user.Email}
			if user.RbacUUID != nil {
				if rbacUser, err := rbacStore.GetUser(*user.RbacUUID); err == nil {
					resp.Root = rbacUser.Root
					resp.Permissions = rbacUser.Permissions
				}
			}
			result = append(result, resp)
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// assignPermissionsHandler lets a root user grant or update another api
// user's rbac rights, addressed by the api's own user ID rather than the
// rbac UUID: the UUID is an implementation detail this endpoint manages on
// the caller's behalf, creating one in queel's rbac directory the first
// time a user is assigned any rights at all, and reusing it on every
// later call.
func assignPermissionsHandler(store *Store, rbacStore *rbac.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !requireRoot(w, r) {
			return
		}

		id := r.PathValue("id")
		user, err := store.UserByID(r.Context(), id)
		if err != nil {
			if errors.Is(err, ErrUserNotFound) {
				writeError(w, http.StatusNotFound, "utilisateur introuvable")
				return
			}
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		var req permissionsRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "corps de requête invalide")
			return
		}
		perms := rbac.Permissions{
			CanVote:          req.CanVote,
			CanCreateText:    req.CanCreateText,
			CanCloseText:     req.CanCloseText,
			CanSelect:        req.CanSelect,
			CanEditSelection: req.CanEditSelection,
			CanUpdateText:    req.CanUpdateText,
		}

		var rbacUser *rbac.User
		if user.RbacUUID == nil {
			rbacUser, err = rbacStore.CreateUser(req.Root, perms)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "erreur serveur")
				return
			}
			if err := store.SetRbacUUID(r.Context(), user.ID, rbacUser.ID); err != nil {
				writeError(w, http.StatusInternalServerError, "erreur serveur")
				return
			}
		} else {
			rbacUser, err = rbacStore.UpdateUser(*user.RbacUUID, req.Root, perms)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "erreur serveur")
				return
			}
		}

		writeJSON(w, http.StatusOK, rbacUser)
	}
}

// deleteUserHandler removes an api account entirely, along with its rbac
// directory entry if it had one — an orphaned rbac.User left behind would
// just be dead weight, never reachable by any api account again.
func deleteUserHandler(store *Store, rbacStore *rbac.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFromContext(r)
		if !ok || !claims.Root {
			writeError(w, http.StatusForbidden, "réservé aux administrateurs")
			return
		}

		id := r.PathValue("id")
		if id == claims.Subject {
			writeError(w, http.StatusBadRequest, "vous ne pouvez pas supprimer votre propre compte")
			return
		}

		user, err := store.UserByID(r.Context(), id)
		if err != nil {
			if errors.Is(err, ErrUserNotFound) {
				writeError(w, http.StatusNotFound, "utilisateur introuvable")
				return
			}
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		if user.RbacUUID != nil {
			if err := rbacStore.DeleteUser(*user.RbacUUID); err != nil && !errors.Is(err, rbac.ErrNotFound) {
				writeError(w, http.StatusInternalServerError, "erreur serveur")
				return
			}
		}

		if err := store.DeleteUser(r.Context(), id); err != nil {
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
