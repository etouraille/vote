# Local 3-node queel cluster for the api binary — see README.md. Node
# addresses/ports/data dirs come from node1.env/node2.env/node3.env; secrets
# (DATABASE_URL, JWT_SECRET, ...) come from ../../.env, sourced first so
# nothing here duplicates them. This directory lives at api/queel/deploy/ —
# inside the queel module, but built against the api binary two levels up.
BINARY := vote-api-cluster
ENV := ../../.env
NODES := node1 node2 node3

.PHONY: build build-api up down restart status logs clean

build:
	cd ../.. && go build -o queel/deploy/$(BINARY) .

# build-api builds the regular single-node dev binary (../../vote-api) —
# same source as the cluster binary above, just a different output path;
# whether a given run is clustered is decided at runtime by
# QUEEL_NODE_ADDRESS, not at build time.
build-api:
	cd ../.. && go build -o vote-api .

# up starts all 3 nodes: node1 first (it founds the cluster and bootstraps
# rbac.db's root user), then a short pause before node2/node3 join through
# it — same order the README documents by hand. Re-running `make up` while
# nodes are already running just adds duplicate processes; run `make down`
# first.
up: build
	@first=1; \
	for n in $(NODES); do \
		echo "starting $$n..."; \
		( set -a; . $(ENV); . ./$$n.env; set +a; nohup ./$(BINARY) > $$n.log 2>&1 & echo $$! > $$n.pid ); \
		if [ $$first -eq 1 ]; then sleep 2; first=0; fi; \
	done
	@echo "cluster up — logs in queel/deploy/node{1,2,3}.log, 'make status' to check, 'make logs' to tail"

down:
	@for n in $(NODES); do \
		if [ -f $$n.pid ]; then kill $$(cat $$n.pid) 2>/dev/null || true; rm -f $$n.pid; fi; \
	done
	@echo "cluster stopped"

restart: down up

# status shows whether each node's process (by the pid file up wrote) is
# still alive.
status:
	@for n in $(NODES); do \
		if [ -f $$n.pid ] && kill -0 $$(cat $$n.pid) 2>/dev/null; then \
			echo "$$n: running (pid $$(cat $$n.pid))"; \
		else \
			echo "$$n: stopped"; \
		fi; \
	done

logs:
	tail -n 20 -f $(NODES:=.log)

# clean stops the cluster and wipes its state (each node's local queel data,
# the shared rbac.db plus its -wal/-shm sidecar files, logs, pid files) so
# the next `make up` starts fresh. The built binary is left alone —
# rebuilding it is cheap.
clean: down
	rm -rf $(NODES) rbac.db rbac.db-wal rbac.db-shm $(NODES:=.log) $(NODES:=.pid)
