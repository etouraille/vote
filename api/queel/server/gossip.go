package server

import (
	"encoding/json"
	"net/http"

	"github.com/etouraille/queel/cluster"
)

// GossipHandler exposes a node's Membership for peer gossip exchanges: POST
// the caller's view, get this node's merged view back — one request/response
// round trip exchanges knowledge both ways (push-pull).
func GossipHandler(membership *cluster.Membership) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var theirView []cluster.Member
		if err := json.NewDecoder(r.Body).Decode(&theirView); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		ourView := membership.HandleGossip(theirView)
		writeJSON(w, http.StatusOK, ourView)
	}
}
