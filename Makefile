APP_NAME=ratemylifedecision

.PHONY: db-up db-down migrate-up run-server test

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

migrate-up:
	go run ./cmd/migrate up

run-server:
	go run ./cmd/server

test:
	go test ./...
