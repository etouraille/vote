package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/rbac"
	_ "github.com/lib/pq"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://vote:vote@localhost:5432/vote?sslmode=disable"
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := waitForDB(db, 10*time.Second); err != nil {
		log.Fatalf("database not reachable: %v", err)
	}

	store := NewStore(db)

	queelDataDir := os.Getenv("QUEEL_DATA_DIR")
	if queelDataDir == "" {
		queelDataDir = "./data"
	}
	queelEngine, err := queel.Open(queelDataDir)
	if err != nil {
		log.Fatalf("queel engine: %v", err)
	}
	defer queelEngine.Close()
	textRepo := queel.NewRepository(queelEngine)

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET must be set — it signs every session token, so there's no safe default")
	}

	rbacPath := os.Getenv("QUEEL_RBAC_PATH")
	if rbacPath == "" {
		rbacPath = filepath.Join(queelDataDir, "rbac.json")
	}
	rbacStore, err := rbac.Open(rbacPath)
	if err != nil {
		log.Fatalf("rbac store: %v", err)
	}

	// QUEEL_ROOT_UUID bootstraps the very first root rbac user under a
	// caller-chosen UUID, so an operator has one root identity to assign
	// every other user's rights with before any other rbac user exists to
	// do that assigning. Safe to leave set across restarts — creation is a
	// no-op once that UUID already exists.
	if rootUUID := os.Getenv("QUEEL_ROOT_UUID"); rootUUID != "" {
		if _, err := rbacStore.CreateUserWithID(rootUUID, true, rbac.Permissions{}); err != nil && !errors.Is(err, rbac.ErrAlreadyExists) {
			log.Fatalf("bootstrapping root rbac user: %v", err)
		}
	}

	ollamaBaseURL := os.Getenv("OLLAMA_BASE_URL")
	if ollamaBaseURL == "" {
		ollamaBaseURL = "http://localhost:11434"
	}
	ollamaModel := os.Getenv("OLLAMA_EMBED_MODEL")
	if ollamaModel == "" {
		ollamaModel = "nomic-embed-text"
	}
	embed := newOllamaEmbedder(ollamaBaseURL, ollamaModel)

	qdrantBaseURL := os.Getenv("QDRANT_BASE_URL")
	if qdrantBaseURL == "" {
		qdrantBaseURL = "http://localhost:6333"
	}
	qdrantCollection := os.Getenv("QDRANT_COLLECTION")
	if qdrantCollection == "" {
		qdrantCollection = "texts"
	}
	qdrant := newQdrantClient(qdrantBaseURL, qdrantCollection)
	pruneSuperseded := os.Getenv("SEARCH_PRUNE_SUPERSEDED") == "true"
	searchIndex := newSearchIndexer(embed, qdrant, pruneSuperseded)

	// Search is an enhancement layered on top of the core voting workflow —
	// if Qdrant/Ollama aren't reachable yet, log and keep starting; every
	// other endpoint works regardless, and search degrades until they are.
	if err := qdrant.EnsureCollection(context.Background(), embeddingDimension); err != nil {
		log.Printf("qdrant collection not ready (search will be degraded until it is): %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/register", registerHandler(store))
	mux.HandleFunc("POST /api/auth/login", loginHandler(store, rbacStore, []byte(jwtSecret)))
	mux.HandleFunc("POST /api/auth/confirm", confirmHandler(store))
	mux.HandleFunc("GET /api/me", meHandler(store))
	mux.HandleFunc("GET /api/admin/users", listUsersHandler(store, rbacStore))
	mux.HandleFunc("PUT /api/admin/users/{id}/permissions", assignPermissionsHandler(store, rbacStore))
	mux.HandleFunc("DELETE /api/admin/users/{id}", deleteUserHandler(store, rbacStore))
	mux.HandleFunc("POST /api/texts", createTextHandler(textRepo, searchIndex))
	mux.HandleFunc("GET /api/texts", recentTextsHandler(textRepo))
	mux.HandleFunc("GET /api/texts/search", searchTextsHandler(searchIndex))
	mux.HandleFunc("GET /api/texts/{id}", getTextHandler(textRepo))
	mux.HandleFunc("GET /api/texts/{id}/with-slots", textWithSlotsHandler(textRepo))
	mux.HandleFunc("PUT /api/texts/{id}", updateTextHandler(textRepo))
	mux.HandleFunc("POST /api/texts/{id}/slots", proposeEditHandler(textRepo))
	mux.HandleFunc("GET /api/texts/{id}/slots/{slotId}/fragments", fragmentsForSlotHandler(textRepo))
	mux.HandleFunc("POST /api/texts/{id}/close-round", closeRoundHandler(textRepo, searchIndex))
	mux.HandleFunc("GET /api/fragments/{id}", getFragmentHandler(textRepo))
	mux.HandleFunc("POST /api/fragments/{id}/vote", castVoteHandler(textRepo))

	handler := withCORS(withBodyLimit(requireToken([]byte(jwtSecret), mux)))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("api listening on :%s", port)
	if err := http.ListenAndServe(":"+port, handler); err != nil {
		log.Fatal(err)
	}
}

func waitForDB(db *sql.DB, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var err error
	for time.Now().Before(deadline) {
		if err = db.Ping(); err == nil {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return err
}

type meResponse struct {
	UserID      string           `json:"userId"`
	Email       string           `json:"email"`
	Pseudo      string           `json:"pseudo,omitempty"`
	Root        bool             `json:"root"`
	Permissions rbac.Permissions `json:"permissions"`
}

// meHandler reports who the caller is and what they're allowed to do. Root
// and Permissions come straight from the already-verified JWT claims — no
// lookup needed; the front end uses Root to decide whether to show the
// admin backoffice. Email/Pseudo aren't in the token, so this is the one
// field pair here that costs a DB read.
func meHandler(store *Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		claims, ok := claimsFromContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "token invalide ou expiré")
			return
		}

		user, err := store.UserByID(r.Context(), claims.Subject)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "erreur serveur")
			return
		}

		var pseudo string
		if user.Pseudo != nil {
			pseudo = *user.Pseudo
		}
		writeJSON(w, http.StatusOK, meResponse{
			UserID:      claims.Subject,
			Email:       user.Email,
			Pseudo:      pseudo,
			Root:        claims.Root,
			Permissions: rbac.PermissionsFromBits(claims.Perms),
		})
	}
}
