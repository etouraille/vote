BINARY := vote-api
PORT ?= 8080

.PHONY: build run clean db-up db-down

build:
	go build -o $(BINARY) .

run: build
	set -a; [ -f .env ] && . ./.env; set +a; PORT=$(PORT) ./$(BINARY) 2>&1

clean:
	rm -f $(BINARY)

db-up:
	docker compose up -d

db-down:
	docker compose down
