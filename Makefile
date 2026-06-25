PY_SERVICE_DIR := backend/python-service/alphaflow-core
GO_SERVICE_DIR := backend/go-service
export GO111MODULE := on

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  make py-sync          Sync Python service dependencies"
	@echo "  make py-run           Run Python service"
	@echo "  make py-lint          Run ruff check"
	@echo "  make py-format        Run ruff format"
	@echo "  make py-format-check  Check ruff formatting"
	@echo "  make py-typecheck     Run pyright"
	@echo "  make py-test          Run pytest"
	@echo "  make py-check         Run Python lint, format check, typecheck, and tests"
	@echo "  make go-market-data-run"
	@echo "  make go-market-data-test"
	@echo "  make go-market-data-tidy"
	@echo "  make go-market-data-check"
	@echo "  make redis-up          Start local Redis with Docker"
	@echo "  make redis-down        Stop local Redis"
	@echo "  make redis-logs        Tail Redis logs"
	@echo "  make redis-cli         Open redis-cli in the Redis container"
	@echo "  make market-data-up    Start market-data with Docker"
	@echo "  make market-data-down  Stop market-data"
	@echo "  make market-data-logs  Tail market-data logs"
	@echo "  make stack-up          Start Redis and market-data"
	@echo "  make check            Run all available checks"

.PHONY: py-sync
py-sync:
	cd $(PY_SERVICE_DIR) && uv sync --dev

.PHONY: py-run
py-run:
	cd $(PY_SERVICE_DIR) && uv run python src/alphaflow/main.py

.PHONY: py-lint
py-lint:
	cd $(PY_SERVICE_DIR) && uv run ruff check .

.PHONY: py-format
py-format:
	cd $(PY_SERVICE_DIR) && uv run ruff format .

.PHONY: py-format-check
py-format-check:
	cd $(PY_SERVICE_DIR) && uv run ruff format --check .

.PHONY: py-typecheck
py-typecheck:
	cd $(PY_SERVICE_DIR) && uv run pyright

.PHONY: py-test
py-test:
	cd $(PY_SERVICE_DIR) && uv run pytest

.PHONY: py-check
py-check: py-lint py-format-check py-typecheck py-test

.PHONY: go-market-data-run
go-market-data-run:
	cd $(GO_SERVICE_DIR) && go run ./market-data/cmd/market-data -config market-data/configs/local.toml

.PHONY: go-market-data-test
go-market-data-test:
	cd $(GO_SERVICE_DIR) && go test ./...

.PHONY: go-market-data-tidy
go-market-data-tidy:
	cd $(GO_SERVICE_DIR) && go mod tidy

.PHONY: go-market-data-check
go-market-data-check: go-market-data-test

.PHONY: redis-up
redis-up:
	docker compose up -d redis

.PHONY: redis-down
redis-down:
	docker compose down

.PHONY: redis-logs
redis-logs:
	docker compose logs -f redis

.PHONY: redis-cli
redis-cli:
	docker compose exec redis redis-cli -p 6380

.PHONY: market-data-up
market-data-up:
	docker compose up -d --build market-data

.PHONY: market-data-down
market-data-down:
	docker compose stop market-data

.PHONY: market-data-logs
market-data-logs:
	docker compose logs -f market-data

.PHONY: stack-up
stack-up:
	docker compose up -d --build redis market-data

.PHONY: check
check: py-check go-market-data-check
