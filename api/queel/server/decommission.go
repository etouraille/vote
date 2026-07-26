package server

import (
	"net/http"

	"github.com/etouraille/queel"
	"github.com/etouraille/queel/cluster"
)

type decommissionResponse struct {
	HandedOff int `json:"handedOff"`
}

// DecommissionHandler exposes a way to trigger cluster.Decommission on this
// node before an operator actually stops it — see that function's doc
// comment for what it does and why it matters (turning a planned departure
// into "the cluster never dips below its replication factor" instead of
// "wait for anti-entropy to eventually notice"). self is this node's own
// address (the same value as bootstrap.Config.Self) and replicationFactor
// must match whatever the cluster's Coordinator was built with (see
// bootstrap.ReplicationFactorFromEnv) — a mismatch here would compute the
// wrong future replica set.
//
// This does not touch cluster membership or stop anything itself: self is
// still considered alive until the caller also stops this process
// afterward, at which point membership detects it as dead exactly as an
// unplanned crash would — just without the under-replication window that
// would otherwise open, since the handoff already happened first.
func DecommissionHandler(engine *queel.Engine, membership *cluster.Membership, self cluster.Node, replicationFactor int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		handedOff, err := cluster.Decommission(r.Context(), engine, self, membership.AliveNodes(), replicationFactor)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, decommissionResponse{HandedOff: handedOff})
	}
}
