APP_NAME=ratemylifedecision

.PHONY: dev-up wait-db db-up db-down db-logs db-shell db-reset migrate-up run-server web-install run-web test

dev-up: db-up wait-db migrate-up

wait-db:
	@until docker compose exec -T postgres pg_isready -U postgres -d ratemylifedecision >/dev/null 2>&1; do \
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
	docker compose exec postgres psql -U postgres -d ratemylifedecision

db-reset:
	docker compose down -v
	docker compose up -d postgres
	$(MAKE) wait-db
	$(MAKE) migrate-up

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
