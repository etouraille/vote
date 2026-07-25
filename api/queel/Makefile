BINARY := queeld
PORT ?= 9090
DATA_DIR ?= ./data

CLUSTER_NODES ?= 3
CLUSTER_BASE_PORT ?= 9101
CLUSTER_RF ?= 3
CLUSTER_GOSSIP_INTERVAL ?= 1s
CLUSTER_DIR := .cluster

.PHONY: build run test clean cluster-up cluster-down cluster-status cluster-clean

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
	done
	@echo "cluster of $(CLUSTER_NODES) node(s) started on ports $(CLUSTER_BASE_PORT)-$$(( $(CLUSTER_BASE_PORT) + $(CLUSTER_NODES) - 1 )); logs in $(CLUSTER_DIR)/node-*.log"

# cluster-down stops every node started by cluster-up.
cluster-down:
	@for f in $(CLUSTER_DIR)/*.pid; do \
		[ -f "$$f" ] || continue; \
		pid=$$(cat "$$f"); \
		kill $$pid 2>/dev/null || true; \
		rm -f "$$f"; \
	done
	@echo "cluster stopped"

# cluster-status shows whether each node's process is still running.
cluster-status:
	@for f in $(CLUSTER_DIR)/*.pid; do \
		[ -f "$$f" ] || continue; \
		pid=$$(cat "$$f"); \
		if kill -0 $$pid 2>/dev/null; then state="running"; else state="stopped"; fi; \
		echo "$$f: pid $$pid ($$state)"; \
	done

cluster-clean: cluster-down
	rm -rf $(CLUSTER_DIR)
