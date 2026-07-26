# Local 3-node queel cluster

Three `api` processes on one machine, each storing its own shard under
`api/queel/deploy/nodeN/`, replicated to each other
(`QUEEL_REPLICATION_FACTOR=3` — every key lives on all 3 nodes) via the
queel cluster protocol. All three share the same Postgres database and the
same `rbac.db` (a SQLite database — see queel/rbac's package doc for why
SQLite specifically, instead of the flat JSON file it used to be, matters
for this exact multi-process-one-file setup), so user accounts and rights
are identical no matter which node you talk to — only the `queel` domain
data (texts/rounds/fragments/votes) is what actually gets distributed
across nodes.

Node-specific settings (ports, data dir, node address) live in
`node1.env`/`node2.env`/`node3.env`. Everything else (`DATABASE_URL`,
`JWT_SECRET`, Ollama/Qdrant, …) comes from `api/.env` — nothing secret is
duplicated into these files.

## Makefile (recommended)

From this directory:

```bash
make up       # builds, then starts node1, then node2 and node3 through it
make status   # shows which nodes are still running
make down     # stops all 3
make clean    # down + wipes node1/, node2/, node3/, rbac.db(-wal/-shm), pids
make restart  # down + up
```

`make up` streams all 3 nodes' logs straight to the terminal it's run from
(no per-node log file to go tail elsewhere) — every line already names its
own port, so the 3 nodes stay distinguishable even interleaved. Keep that
terminal open for as long as the cluster should stay up: nothing shields
these processes from a SIGHUP anymore.

## Manual equivalent

```bash
cd api/queel/deploy
go build -o vote-api-cluster ../..

( set -a; . ../../.env; . ./node1.env; set +a; ./vote-api-cluster ) &
sleep 2
( set -a; . ../../.env; . ./node2.env; set +a; ./vote-api-cluster ) &
( set -a; . ../../.env; . ./node3.env; set +a; ./vote-api-cluster ) &
```

Each should log `api listening on :PORT (clustered: true)` plus a
`queel internal replication listening on :INTERNAL_PORT` line. Stop them
all with `kill %1 %2 %3`.

## Talking to it

Any of the 3 public ports behaves identically — same routes, same rbac
protection, same data (once replicated):

```bash
curl -s http://localhost:8091/api/auth/login -H 'Content-Type: application/json' \
  -d '{"email":"...","password":"..."}'

curl -s http://localhost:8092/api/texts -H "Authorization: Bearer $TOKEN"
```

Each node also answers an unauthenticated `GET /healthz` — Postgres and the
queel store's reachability, plus how many cluster members it currently
believes are alive:

```bash
curl -s http://localhost:8091/healthz
# {"status":"ok","checks":{"postgres":"ok","queel":"ok"},"clustered":true,"aliveNodes":3}
```

A text created via node 1 is immediately readable via node 2 and node 3.
Killing any one node still leaves reads/writes working through the other
two (quorum = 2 of 3). A node that rejoins catches up two ways: read-repair
fixes a key the moment something re-reads it, and the background
anti-entropy job (`QUEEL_ANTI_ENTROPY_INTERVAL`, 5s in these node*.env
files — see `queel/cluster.RunAntiEntropy`) fixes everything else within
one interval by comparing its whole keyspace against a random peer's via a
Merkle tree, whether or not anyone ever reads those keys again. Wipe a
node's data dir entirely (`rm -rf node3`) and restart it — it recovers its
full keyspace from peers within `QUEEL_ANTI_ENTROPY_INTERVAL` on its own.

## Decommissioning a node cleanly

Killing a node outright works (that's what the paragraph above is about),
but it leaves a window — up to `QUEEL_ANTI_ENTROPY_INTERVAL` — where the
cluster runs under-replicated before anti-entropy notices and repairs it.
When a node's departure is planned (scaling down, replacing an instance),
avoid that window instead: `POST` to its *internal* port before stopping
it, so its data is handed off first.

```bash
curl -s -X POST http://localhost:9193/internal/decommission
# {"handedOff":4}
```

This walks every key node 3 holds locally and pushes it to whichever
node(s) will be responsible for it once node 3 is gone (its future replica
set, computed the same way the ring always is — see `cluster.Decommission`),
skipping anything a target already has an equal-or-newer copy of. Only
*after* this returns should you actually stop the process — membership then
detects the departure exactly as it would an unplanned crash, but the
cluster never dipped below its replication factor in between.

### Triggering it automatically

The `curl` above is manual — it only protects a departure if whoever stops
the node remembers to run it first. A node can't predict a `SIGKILL`, a
crash, or a power loss (nothing can be caught, so anti-entropy stays the
only safety net for those), but it *can* catch its own graceful shutdown:
`docker stop`, `kubectl delete pod`, and `systemctl stop` all send
`SIGTERM` before escalating to `SIGKILL`, so a node that traps `SIGTERM`
can decommission itself in the moment it's asked to stop, instead of
depending on the operator to call the endpoint above first.

That's what `cluster.DecommissionOnShutdown` does: registered once in
clustered mode, it blocks on `SIGTERM`/`SIGINT` in the background and,
the moment one arrives, runs the exact same handoff as the manual `curl`
against this node's own engine, then exits. It's bounded by
`QUEEL_DECOMMISSION_TIMEOUT` (default 25s, just under Kubernetes'
default 30s `terminationGracePeriodSeconds`) — if the handoff hasn't
finished by then, it gives up and exits anyway rather than risk being
`SIGKILL`ed mid-handoff, which would leave whatever wasn't handed off yet
to anti-entropy exactly as an unplanned crash would. The internal
`/internal/decommission` endpoint is still there for anyone who wants to
trigger it ahead of time by hand instead.

## Cleaning up

`node1/`, `node2/`, `node3/` (local engine data) and `rbac.db` (plus its
`-wal`/`-shm` sidecar files), all directly under `api/queel/deploy/`, are
this cluster's state — `make clean` wipes them, or by hand:

```bash
rm -rf node1 node2 node3 rbac.db rbac.db-wal rbac.db-shm *.pid
```
