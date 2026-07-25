package main

import (
	"database/sql"
	"net/http"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/cluster"
)

// healthzKey is queried (never written) by healthHandler to exercise the
// queel store's read path — a key chosen to never legitimately exist, so a
// "not found" result (found=false, err=nil) is exactly what a healthy store
// returns.
var healthzKey = []byte("__healthz__")

type healthResponse struct {
	Status     string            `json:"status"`
	Checks     map[string]string `json:"checks"`
	Clustered  bool              `json:"clustered"`
	AliveNodes int               `json:"aliveNodes,omitempty"`
}

// healthHandler answers GET /healthz — deliberately mounted outside the
// /api/... prefix (see requireToken in middleware.go) so orchestration
// (a Kubernetes liveness/readiness probe, a load balancer's own health
// check) can hit it without a bearer token. It reports two dependencies
// this process actually needs to serve traffic: Postgres (user accounts,
// auth) and the queel store (texts/rounds/fragments/votes — a
// *cluster.DistributedStore in cluster mode, so this also exercises that
// this node can currently reach enough peers for a quorum read). Search
// (Qdrant/Ollama) is deliberately not checked here — see main.go's own
// comment on why it's allowed to degrade instead of blocking startup;
// treating it as a hard health-check failure would contradict that.
func healthHandler(db *sql.DB, queelStore queel.Store, membership *cluster.Membership) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		healthy := true
		checks := make(map[string]string, 2)

		if err := db.PingContext(r.Context()); err != nil {
			checks["postgres"] = "error: " + err.Error()
			healthy = false
		} else {
			checks["postgres"] = "ok"
		}

		if _, _, err := queelStore.Get(healthzKey); err != nil {
			checks["queel"] = "error: " + err.Error()
			healthy = false
		} else {
			checks["queel"] = "ok"
		}

		resp := healthResponse{Status: "ok", Checks: checks}
		if !healthy {
			resp.Status = "unhealthy"
		}
		if membership != nil {
			resp.Clustered = true
			resp.AliveNodes = len(membership.AliveNodes())
		}

		status := http.StatusOK
		if !healthy {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, resp)
	}
}
