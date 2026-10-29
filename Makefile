TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:55432/postgres?sslmode=disable
COMPOSE_TEST = docker compose -f docker-compose.test.yml

.PHONY: test test-db-up test-db-down up down

test-db-up:
	$(COMPOSE_TEST) up -d --wait

test-db-down:
	$(COMPOSE_TEST) down -v

test: test-db-up
	cd backend && TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test ./...
	@if [ -f frontend/package.json ]; then cd frontend && npm test; else echo "frontend: skipped (no package.json)"; fi

up:
	docker compose up -d --build

down:
	docker compose down
