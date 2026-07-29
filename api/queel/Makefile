BINARY := queeld
PORT ?= 9090
DATA_DIR ?= ./data

CLUSTER_NODES ?= 3
CLUSTER_BASE_PORT ?= 9101
CLUSTER_RF ?= 3
CLUSTER_GOSSIP_INTERVAL ?= 1s
CLUSTER_DIR := .cluster

# How long cluster-down waits for one node to exit after SIGTERM before
# escalating to SIGKILL. Deliberately above cluster.DefaultDecommissionTimeout
# (25s), so a node busy handing its data off is never cut short by this.
CLUSTER_STOP_TIMEOUT ?= 30

# How long cluster-up waits for the seed node to start answering before
# launching the others anyway.
CLUSTER_SEED_TIMEOUT ?= 15

.PHONY: build run test clean cluster-up wait-for-seed cluster-check cluster-down cluster-orphans cluster-status cluster-clean

build:
	go build -o $(BINARY) ./cmd/queeld

run: build
	PORT=$(PORT) QUEEL_DATA_DIR=$(DATA_DIR) ./$(BINARY)

test:
	go test ./...

clean:
	rm -f $(BINARY)
	rm -rf $(DATA_DIR)

# cluster-up starts CLUSTER_NODES queeld processes in the background, each on
# its own port and data directory. Only the first node has no seed — every
# other node joins by gossiping through it, never via a static full node
# list. Override node count / ports / replication factor as needed, e.g.:
#   make cluster-up CLUSTER_NODES=5 CLUSTER_BASE_PORT=9200
#
# The others only start once the seed answers. Starting them all at once
# raced the seed's own startup: every later node failed its join with
# "connection refused" and exited immediately, leaving a one-node cluster
# behind a message claiming three had started.
#
# Waiting for an answer rather than sleeping a fixed couple of seconds:
# the right delay isn't knowable in advance, and a loaded machine loses
# the race a constant would have won.
cluster-up: build
	@mkdir -p $(CLUSTER_DIR)
	@for i in $$(seq 1 $(CLUSTER_NODES)); do \
		port=$$(( $(CLUSTER_BASE_PORT) + i - 1 )); \
		addr="http://localhost:$$port"; \
		if [ $$i -eq 1 ]; then seed=""; else seed="http://localhost:$(CLUSTER_BASE_PORT)"; fi; \
		echo "starting node $$i on $$addr (seed: $${seed:-none})"; \
		QUEEL_NODE_ADDRESS=$$addr QUEEL_SEED_NODE=$$seed QUEEL_REPLICATION_FACTOR=$(CLUSTER_RF) \
		QUEEL_GOSSIP_INTERVAL=$(CLUSTER_GOSSIP_INTERVAL) QUEEL_DATA_DIR=$(CLUSTER_DIR)/node-$$i \
		PORT=$$port ./$(BINARY) > $(CLUSTER_DIR)/node-$$i.log 2>&1 & \
		echo $$! > $(CLUSTER_DIR)/node-$$i.pid; \
		if [ $$i -eq 1 ] && [ $(CLUSTER_NODES) -gt 1 ]; then \
			$(MAKE) --no-print-directory wait-for-seed SEED_URL="$$addr"; \
		fi; \
	done
	@$(MAKE) --no-print-directory cluster-check

# wait-for-seed blocks until the seed accepts a connection. Any HTTP reply
# proves the port is open, so the probe doesn't care which status it gets —
# it only needs curl to connect. Without curl it falls back to a fixed
# pause, which is what this used to do unconditionally.
wait-for-seed:
	@if ! command -v curl >/dev/null 2>&1; then sleep 2; exit 0; fi; \
	waited=0; \
	while [ $$waited -lt $(CLUSTER_SEED_TIMEOUT) ]; do \
		curl -s -o /dev/null --max-time 1 "$(SEED_URL)/" 2>/dev/null && exit 0; \
		sleep 1; waited=$$((waited + 1)); \
	done; \
	echo "warning: seed $(SEED_URL) silent after $(CLUSTER_SEED_TIMEOUT)s — starting the others anyway" >&2

# cluster-check reports which nodes actually survived startup. A node that
# fails to join exits on the spot, and without this the closing "cluster of
# N node(s) started" would claim a cluster that isn't there — exactly the
# lie the missing pause used to produce.
cluster-check:
	@sleep 1; \
	alive=0; dead=""; \
	for f in $(CLUSTER_DIR)/*.pid; do \
		[ -f "$$f" ] || continue; \
		pid=$$(cat "$$f"); \
		if kill -0 $$pid 2>/dev/null; then \
			alive=$$((alive + 1)); \
		else \
			dead="$$dead $$(basename $$f .pid)"; \
		fi; \
	done; \
	if [ -n "$$dead" ]; then \
		echo "warning: node(s) died on startup:$$dead — see $(CLUSTER_DIR)/node-*.log" >&2; \
	fi; \
	echo "cluster: $$alive node(s) running on ports $(CLUSTER_BASE_PORT)-$$(( $(CLUSTER_BASE_PORT) + $(CLUSTER_NODES) - 1 )); logs in $(CLUSTER_DIR)/node-*.log"; \
	[ -z "$$dead" ]

# cluster-down stops every node started by cluster-up, one at a time, and
# makes sure each is actually gone before saying so.
#
# SIGTERM, not SIGKILL: only SIGTERM and SIGINT are trapped (see
# cluster.DecommissionOnShutdown), and that trap is what hands a departing
# node's data off to its peers. SIGKILL cannot be caught, so it would skip
# the handoff entirely — the very data loss stopping nodes carefully is
# meant to avoid. It stays here only as a last resort, for a node that
# refuses to leave, and says so when it comes to that.
#
# One node at a time falls out of waiting for each to exit before moving
# on: a departing node hands its data to nodes that are still alive, so
# signalling all three at once races several simultaneous departures
# against a shrinking set of survivors.
#
# The pid file is removed only once the process is confirmed dead. Deleting
# it after a failed kill throws away the only handle on a node that is
# still running, leaving cluster-status to report it as stopped while it
# still holds its port.
cluster-down:
	@stopped=0; failed=0; \
	for f in $(CLUSTER_DIR)/*.pid; do \
		[ -f "$$f" ] || continue; \
		pid=$$(cat "$$f"); \
		if [ -z "$$pid" ] || ! kill -0 $$pid 2>/dev/null; then \
			echo "$$f: not running"; \
			rm -f "$$f"; \
			continue; \
		fi; \
		kill -TERM $$pid 2>/dev/null || true; \
		waited=0; \
		while kill -0 $$pid 2>/dev/null && [ $$waited -lt $(CLUSTER_STOP_TIMEOUT) ]; do \
			sleep 1; waited=$$((waited + 1)); \
		done; \
		if kill -0 $$pid 2>/dev/null; then \
			echo "$$f: still alive $(CLUSTER_STOP_TIMEOUT)s after SIGTERM — SIGKILL, decommission incomplete" >&2; \
			kill -KILL $$pid 2>/dev/null || true; \
			sleep 1; \
		fi; \
		if kill -0 $$pid 2>/dev/null; then \
			echo "$$f: FAILED to stop (pid $$pid) — pid file kept" >&2; \
			failed=$$((failed + 1)); \
		else \
			echo "$$f: stopped (pid $$pid, after $${waited}s)"; \
			rm -f "$$f"; \
			stopped=$$((stopped + 1)); \
		fi; \
	done; \
	echo "cluster-down: $$stopped stopped, $$failed still running"; \
	[ $$failed -eq 0 ]
	@$(MAKE) --no-print-directory cluster-orphans

# cluster-orphans reports any queeld process no pid file accounts for —
# the state a kill that failed while its pid file was deleted leaves
# behind, and which cluster-status alone cannot see.
cluster-orphans:
	@leftover=$$(pgrep -x $(BINARY) 2>/dev/null | tr '\n' ' '); \
	tracked=""; \
	for f in $(CLUSTER_DIR)/*.pid; do [ -f "$$f" ] && tracked="$$tracked $$(cat $$f)"; done; \
	orphans=""; \
	for pid in $$leftover; do \
		case " $$tracked " in *" $$pid "*) ;; *) orphans="$$orphans $$pid";; esac; \
	done; \
	if [ -n "$$orphans" ]; then \
		echo "warning: $(BINARY) running without a pid file:$$orphans" >&2; \
		echo "         stop them with: kill -TERM$$orphans" >&2; \
	fi

# cluster-status shows whether each node's process is still running.
cluster-status:
	@found=0; \
	for f in $(CLUSTER_DIR)/*.pid; do \
		[ -f "$$f" ] || continue; \
		found=1; \
		pid=$$(cat "$$f"); \
		if kill -0 $$pid 2>/dev/null; then state="running"; else state="stopped"; fi; \
		echo "$$f: pid $$pid ($$state)"; \
	done; \
	[ $$found -eq 1 ] || echo "no pid files in $(CLUSTER_DIR)/ — cluster not started from here"
	@$(MAKE) --no-print-directory cluster-orphans

cluster-clean: cluster-down
	rm -rf $(CLUSTER_DIR)
