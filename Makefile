.PHONY: build run test smoke compose-up compose-down clean

BINARY := bin/osh

build:
	go build -o $(BINARY) ./cmd/osh

run:
	go run ./cmd/osh

test:
	go test ./...

# Start the process, hit /healthz and a create/list round-trip, then stop.
smoke: build
	@./scripts/smoke.sh

compose-up:
	docker compose up --build -d

compose-down:
	docker compose down

clean:
	rm -rf bin
