PY_SERVICE_DIR := backend/python-service/alphaflow-core
GO_SERVICE_DIR := backend/go-service
GO_SERVICE_BIN_DIR := $(GO_SERVICE_DIR)/bin
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
	@echo "  make go-market-data-admin"
	@echo "  make go-market-data-build"
	@echo "  make go-market-data-clean"
	@echo "  make go-market-data-test"
	@echo "  make go-market-data-tidy"
	@echo "  make go-market-data-check"
	@echo "  make go-strategy-engine-run"
	@echo "  make go-strategy-engine-build"
	@echo "  make redis-up          Start local Redis with Docker"
	@echo "  make redis-down        Stop local Redis"
	@echo "  make redis-logs        Tail Redis logs"
	@echo "  make redis-cli         Open redis-cli in the Redis container"
	@echo "  make clickhouse-up     Start local ClickHouse with Docker"
	@echo "  make clickhouse-down   Stop local ClickHouse"
	@echo "  make clickhouse-logs   Tail ClickHouse logs"
	@echo "  make clickhouse-client Open clickhouse-client in the ClickHouse container"
	@echo "  make postgres-up       Start local PostgreSQL with Docker"
	@echo "  make postgres-down     Stop local PostgreSQL"
	@echo "  make postgres-logs     Tail PostgreSQL logs"
	@echo "  make postgres-shell    Open psql in the PostgreSQL container"
	@echo "  make market-data-up    Start market-data with Docker"
	@echo "  make market-data-down  Stop market-data"
	@echo "  make market-data-logs  Tail market-data logs"
	@echo "  make stack-up          Start Redis, ClickHouse, PostgreSQL, and market-data"
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

.PHONY: go-market-data-admin
go-market-data-admin:
	cd $(GO_SERVICE_DIR) && go run ./market-data/cmd/market-data-admin --config market-data/configs/local.toml $(ARGS)

.PHONY: go-market-data-build
go-market-data-build:
	mkdir -p $(GO_SERVICE_BIN_DIR)
	cd $(GO_SERVICE_DIR) && go build -o bin/market-data ./market-data/cmd/market-data
	cd $(GO_SERVICE_DIR) && go build -o bin/market-data-admin ./market-data/cmd/market-data-admin
	cd $(GO_SERVICE_DIR) && go build -o bin/market-data-symbols ./market-data/cmd/market-data-symbols
	cd $(GO_SERVICE_DIR) && go build -o bin/market-data-loadtest ./market-data/cmd/market-data-loadtest
	cd $(GO_SERVICE_DIR) && go build -o bin/market-data-indicator-loadtest ./market-data/cmd/market-data-indicator-loadtest

.PHONY: go-market-data-clean
go-market-data-clean:
	rm -rf $(GO_SERVICE_BIN_DIR)

.PHONY: go-market-data-test
go-market-data-test:
	cd $(GO_SERVICE_DIR) && go test ./...

.PHONY: go-market-data-tidy
go-market-data-tidy:
	cd $(GO_SERVICE_DIR) && go mod tidy

.PHONY: go-market-data-check
go-market-data-check: go-market-data-test

.PHONY: go-strategy-engine-run
go-strategy-engine-run:
	cd $(GO_SERVICE_DIR) && go run ./strategy-engine/cmd/strategy-engine -config strategy-engine/configs/local.toml

.PHONY: go-strategy-engine-build
go-strategy-engine-build:
	mkdir -p $(GO_SERVICE_BIN_DIR)
	cd $(GO_SERVICE_DIR) && go build -o bin/strategy-engine ./strategy-engine/cmd/strategy-engine

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

.PHONY: clickhouse-up
clickhouse-up:
	docker compose up -d clickhouse

.PHONY: clickhouse-down
clickhouse-down:
	docker compose stop clickhouse

.PHONY: clickhouse-logs
clickhouse-logs:
	docker compose logs -f clickhouse

.PHONY: clickhouse-client
clickhouse-client:
	docker compose exec clickhouse clickhouse-client

.PHONY: postgres-up
postgres-up:
	docker compose up -d postgres

.PHONY: postgres-down
postgres-down:
	docker compose stop postgres

.PHONY: postgres-logs
postgres-logs:
	docker compose logs -f postgres

.PHONY: postgres-shell
postgres-shell:
	docker compose exec postgres psql -U alphaflow -d alphaflow

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
	docker compose up -d --build redis clickhouse postgres market-data

.PHONY: check
check: py-check go-market-data-check
