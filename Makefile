APP_NAME=ratemylifedecision

ifneq (,$(wildcard .env))
include .env
endif

PORT ?= 8080
POSTGRES_DB ?= ratemylifedecision
POSTGRES_USER ?= postgres
POSTGRES_PASSWORD ?= postgres
POSTGRES_HOST_PORT ?= 5432
DATABASE_URL ?= postgres://$(POSTGRES_USER):$(POSTGRES_PASSWORD)@localhost:$(POSTGRES_HOST_PORT)/$(POSTGRES_DB)?sslmode=disable

.PHONY: env-init dev-up wait-db db-up db-down db-logs db-shell db-reset migrate-up run-server web-install run-web test

env-init:
	@test -f .env || cp .env.example .env

dev-up: env-init db-up wait-db migrate-up

wait-db:
	@until docker compose exec -T postgres pg_isready -U $(POSTGRES_USER) -d $(POSTGRES_DB) >/dev/null 2>&1; do \
		echo "Waiting for postgres to be ready..."; \
		sleep 1; \
	done
	@echo "Postgres is ready."

db-up:
	docker compose up -d postgres

db-down:
	docker compose down

db-logs:
	docker compose logs -f postgres

db-shell:
	docker compose exec postgres psql -U $(POSTGRES_USER) -d $(POSTGRES_DB)

db-reset:
	docker compose down -v
	docker compose up -d postgres
	$(MAKE) wait-db
	$(MAKE) migrate-up

migrate-up:
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/migrate up

run-server:
	PORT=$(PORT) DATABASE_URL=$(DATABASE_URL) OPENAI_API_KEY=$(OPENAI_API_KEY) go run ./cmd/server

web-install:
	npm --prefix web install

run-web:
	PORT=3000 npm --prefix web run dev

test:
	go test ./...
