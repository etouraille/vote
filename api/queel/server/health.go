package server

import (
	"net/http"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/cluster"
)

// healthzKey is queried (never written) by HealthHandler to exercise
// store's read path — a key chosen to never legitimately exist, so a
// "not found" result (found=false, err=nil) is exactly what a healthy
// store returns.
var healthzKey = []byte("__healthz__")

type healthResponse struct {
	Status     string `json:"status"`
	Store      string `json:"store"`
	Clustered  bool   `json:"clustered"`
	AliveNodes int    `json:"aliveNodes,omitempty"`
}

// HealthHandler answers a GET /healthz for a queel server: whether store
// (a *queel.Engine locally, a *cluster.DistributedStore — so this also
// exercises reaching enough peers for a quorum read — in cluster mode) is
// reachable, and how many cluster members this node currently believes are
// alive when membership is non-nil (pass nil for a single, unclustered
// node). Meant for orchestration (a Kubernetes probe, a load balancer's own
// health check) — mount it unauthenticated, same as NewInternalHandler's
// routes, since neither this repo's api nor queeld itself gates it behind
// a bearer token.
func HealthHandler(store queel.Store, membership *cluster.Membership) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		resp := healthResponse{Status: "ok", Store: "ok"}

		if _, _, err := store.Get(healthzKey); err != nil {
			resp.Status = "unhealthy"
			resp.Store = "error: " + err.Error()
		}

		if membership != nil {
			resp.Clustered = true
			resp.AliveNodes = len(membership.AliveNodes())
		}

		status := http.StatusOK
		if resp.Status != "ok" {
			status = http.StatusServiceUnavailable
		}
		writeJSON(w, status, resp)
	}
}
