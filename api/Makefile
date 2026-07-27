BINARY := vote-api
PORT ?= 8080

.PHONY: build run kill clean db-up db-down

build:
	go build -o $(BINARY) .

run: build
	set -a; [ -f .env ] && . ./.env; set +a; PORT=$(PORT) ./$(BINARY) 2>&1

# Stops whatever's listening on PORT (a previous `make run`, most likely) —
# handy before starting a fresh one instead of hunting the pid by hand.
kill:
	lsof -ti:$(PORT) -sTCP:LISTEN | xargs -r kill

clean:
	rm -f $(BINARY)

db-up:
	docker compose up -d

db-down:
	docker compose down
