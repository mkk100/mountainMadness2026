APP_NAME=ratemylifedecision

.PHONY: db-up db-down migrate-up run-server web-install run-web test

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

migrate-up:
	go run ./cmd/migrate up

run-server:
	go run ./cmd/server

web-install:
	npm --prefix web install

run-web:
	npm --prefix web run dev

test:
	go test ./...
