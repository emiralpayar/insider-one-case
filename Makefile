TEST_DATABASE_URL ?= postgres://postgres:postgres@localhost:55432/postgres?sslmode=disable
COMPOSE_TEST = docker compose -f docker-compose.test.yml

.PHONY: test test-db-up test-db-down up down logs eval chaos

test-db-up:
	$(COMPOSE_TEST) up -d --wait

test-db-down:
	$(COMPOSE_TEST) down -v

# Same checks as CI: go vet (incl. the eval/live tagged files), go test with
# the race detector, frontend tests and the type-checked build.
test: test-db-up
	cd backend && go vet ./... && go vet -tags=eval,live ./internal/llm && TEST_DATABASE_URL='$(TEST_DATABASE_URL)' go test -race ./...
	@if [ -f frontend/package.json ]; then cd frontend && { [ -d node_modules ] || npm ci; } && npm test && npm run build; else echo "frontend: skipped (no package.json)"; fi

# Whole system in the background; returns once every service is healthy.
up:
	docker compose up -d --build --wait

down:
	docker compose down -v

logs:
	docker compose logs -f api worker

# Golden brief set against the real Claude API (costs a few cents; needs
# ANTHROPIC_API_KEY in .env). EVAL_CONTENT=0 skips content generation.
eval:
	@[ -f .env ] || { echo "eval: .env not found (copy .env.example and set ANTHROPIC_API_KEY)"; exit 1; }
	cd backend && set -a && . ../.env && set +a && go test -tags=eval -run TestEval -count=1 -v -timeout 30m ./internal/llm

# Real crash test against the running stack (make up first): SIGKILLs the
# SMS-leader worker mid-campaign and checks takeover, recovery and that no
# message was delivered twice. Adds one "Chaos test" campaign.
chaos:
	./scripts/chaos.sh
