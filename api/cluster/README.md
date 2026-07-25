# Local 3-node queel cluster

Three `api` processes on one machine, each storing its own shard under
`api/cluster/nodeN/`, replicated to each other (`QUEEL_REPLICATION_FACTOR=3`
— every key lives on all 3 nodes) via the queel cluster protocol. All three
share the same Postgres database and the same `rbac.json`, so user accounts
and rights are identical no matter which node you talk to — only the
`queel` domain data (texts/rounds/fragments/votes) is what actually gets
distributed across nodes.

Node-specific settings (ports, data dir, node address) live in
`node1.env`/`node2.env`/`node3.env`. Everything else (`DATABASE_URL`,
`JWT_SECRET`, Ollama/Qdrant, …) comes from `../.env` — nothing secret is
duplicated into these files.

## Makefile (recommended)

From this directory:

```bash
make up       # builds, then starts node1, then node2 and node3 through it
make status   # shows which nodes are still running
make logs     # tails all 3 logs
make down     # stops all 3
make clean    # down + wipes node1/, node2/, node3/, rbac.json, logs, pids
make restart  # down + up
```

## Manual equivalent

```bash
cd api/cluster
go build -o vote-api-cluster ..

( set -a; . ../.env; . ./node1.env; set +a; ./vote-api-cluster ) &
sleep 2
( set -a; . ../.env; . ./node2.env; set +a; ./vote-api-cluster ) &
( set -a; . ../.env; . ./node3.env; set +a; ./vote-api-cluster ) &
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

A text created via node 1 is immediately readable via node 2 and node 3.
Killing any one node still leaves reads/writes working through the other
two (quorum = 2 of 3); the killed node catches up automatically via
read-repair once it rejoins.

## Cleaning up

`node1/`, `node2/`, `node3/` (local engine data) and `rbac.json`, all
directly under `api/cluster/`, are this cluster's state — `make clean`
wipes them, or by hand:

```bash
rm -rf node1 node2 node3 rbac.json *.log *.pid
```
