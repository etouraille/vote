# Local 3-node queel cluster for the api binary — see README.md. Node
# addresses/ports/data dirs come from node1.env/node2.env/node3.env; secrets
# (DATABASE_URL, JWT_SECRET, ...) come from ../../.env, sourced first so
# nothing here duplicates them. This directory lives at api/queel/deploy/ —
# inside the queel module, but built against the api binary two levels up.
BINARY := vote-api-cluster
ENV := ../../.env
NODES := node1 node2 node3

.PHONY: build build-api up down check-orphans kill-node kill-all restart status clean

# How long kill-all waits between one node's SIGTERM and the next's.
KILL_DELAY ?= 5

# How long down waits for a node to exit after SIGTERM before escalating to
# SIGKILL. Generous, because SIGTERM triggers a decommission that hands this
# node's data off to its peers — cutting that short costs replication.
STOP_TIMEOUT ?= 15

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
# it — same order the README documents by hand. Each node's stdout/stderr
# goes straight to this terminal (no per-node log file to go tail elsewhere,
# and deliberately no nohup — nohup redirects to nohup.out the moment
# stdout isn't already redirected to something else, which would silently
# defeat this) — every line already names its own port (see main.go's
# startup log), so the 3 nodes stay distinguishable even interleaved. Since
# nothing keeps them alive past a SIGHUP anymore, keep the terminal running
# `make up` open for as long as the cluster should stay up.
#
# Refuses to start on top of a node that is already running: doing so used
# to spawn a duplicate that couldn't bind its port and died, while the pid
# file was overwritten with the dead one's pid — leaving the real node
# untracked and `down` unable to reach it.
up: build
	@for n in $(NODES); do \
		if [ -f $$n.pid ] && kill -0 $$(cat $$n.pid) 2>/dev/null; then \
			echo "$$n already running (pid $$(cat $$n.pid)) — run 'make down' first" >&2; \
			exit 1; \
		fi; \
	done
	@$(MAKE) --no-print-directory check-orphans
	@first=1; \
	for n in $(NODES); do \
		echo "starting $$n..."; \
		( set -a; . $(ENV); . ./$$n.env; set +a; ./$(BINARY) & echo $$! > $$n.pid ); \
		if [ $$first -eq 1 ]; then sleep 2; first=0; fi; \
	done
	@echo "cluster up — logs streaming above, 'make status' to check"

# down stops every node and, unlike a bare `kill`, makes sure they are
# actually gone before saying so.
#
# It waits for each process to exit rather than firing SIGTERM and moving
# on: a node handles SIGTERM by decommissioning (see kill-node), which takes
# time, and `down` immediately followed by `up` would otherwise start new
# nodes while the old ones still hold the ports.
#
# The pid file is removed only once the process is confirmed dead. Deleting
# it on a failed kill was the real trap: it threw away the only handle on a
# node that was still running, after which `status` reported "stopped" and
# the port stayed busy with no way left to name the culprit.
down:
	@stopped=0; failed=0; \
	for n in $(NODES); do \
		pid=""; \
		[ -f $$n.pid ] && pid=$$(cat $$n.pid); \
		if [ -z "$$pid" ] || ! kill -0 $$pid 2>/dev/null; then \
			echo "$$n: not running"; \
			rm -f $$n.pid; \
			continue; \
		fi; \
		kill -TERM $$pid 2>/dev/null || true; \
		waited=0; \
		while kill -0 $$pid 2>/dev/null && [ $$waited -lt $(STOP_TIMEOUT) ]; do \
			sleep 1; waited=$$((waited + 1)); \
		done; \
		if kill -0 $$pid 2>/dev/null; then \
			echo "$$n: still alive $(STOP_TIMEOUT)s after SIGTERM, sending SIGKILL"; \
			kill -KILL $$pid 2>/dev/null || true; \
			sleep 1; \
		fi; \
		if kill -0 $$pid 2>/dev/null; then \
			echo "$$n: FAILED to stop (pid $$pid) — pid file kept" >&2; \
			failed=$$((failed + 1)); \
		else \
			echo "$$n: stopped (pid $$pid, after $${waited}s)"; \
			rm -f $$n.pid; \
			stopped=$$((stopped + 1)); \
		fi; \
	done; \
	echo "cluster down: $$stopped stopped, $$failed still running"; \
	[ $$failed -eq 0 ]
	@$(MAKE) --no-print-directory check-orphans

# check-orphans reports any node process no pid file accounts for.
#
# That is the state a previously broken `down` used to leave behind, and it
# is invisible otherwise: `status` only knows about pid files, so a node
# running without one looks stopped while still holding its port.
#
# Matched on the anchored command line rather than the process name: Linux
# truncates process names to 15 characters, so `pgrep -x vote-api-cluster`
# silently matches nothing at all. The anchors keep the `go build` that
# writes this same path, and make's own shell, from counting as nodes.
check-orphans:
	@leftover=$$(pgrep -f "^\./$(BINARY)$$" 2>/dev/null | tr '\n' ' '); \
	tracked=""; \
	for n in $(NODES); do [ -f $$n.pid ] && tracked="$$tracked $$(cat $$n.pid)"; done; \
	orphans=""; \
	for pid in $$leftover; do \
		case " $$tracked " in *" $$pid "*) ;; *) orphans="$$orphans $$pid";; esac; \
	done; \
	if [ -n "$$orphans" ]; then \
		echo "warning: $(BINARY) running without a pid file:$$orphans" >&2; \
		echo "         stop them with: kill -TERM$$orphans" >&2; \
	fi

# kill-node stops a single node with SIGTERM (make kill-node NODE=node3) —
# unlike `down` (all 3 at once), this is for exercising a single node's
# graceful departure by hand: SIGTERM is exactly what triggers
# cluster.DecommissionOnShutdown (see queeld/main.go's doc comment),
# handing this node's data off to the others before the process actually
# exits instead of leaving the cluster under-replicated until anti-entropy
# eventually notices.
kill-node:
	@if [ -z "$(NODE)" ]; then echo "usage: make kill-node NODE=node1|node2|node3" >&2; exit 1; fi
	@if [ -f $(NODE).pid ]; then \
		kill -TERM $$(cat $(NODE).pid) 2>/dev/null || true; \
		rm -f $(NODE).pid; \
		echo "$(NODE): sent SIGTERM"; \
	else \
		echo "$(NODE): no pid file (not running?)" >&2; \
	fi

# kill-all stops every node one at a time (SIGTERM, via kill-node) instead
# of all at once like `down` — pausing KILL_DELAY seconds between each so a
# departing node's decommission (see kill-node's doc comment) has time to
# finish handing its data off to whichever nodes are still alive before the
# next one leaves too, rather than racing several simultaneous departures
# against a shrinking set of survivors.
kill-all:
	@last=""; for n in $(NODES); do last=$$n; done; \
	for n in $(NODES); do \
		$(MAKE) --no-print-directory kill-node NODE=$$n; \
		if [ "$$n" != "$$last" ]; then sleep $(KILL_DELAY); fi; \
	done
	@echo "cluster stopped (one node at a time, $(KILL_DELAY)s apart)"

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
	@$(MAKE) --no-print-directory check-orphans

# clean stops the cluster and wipes its state (each node's local queel data,
# the shared rbac.db plus its -wal/-shm sidecar files, pid files) so the
# next `make up` starts fresh. The built binary is left alone — rebuilding
# it is cheap.
clean: down
	rm -rf $(NODES) rbac.db rbac.db-wal rbac.db-shm $(NODES:=.pid)
