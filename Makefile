GO_SERVICE_DIR := backend/go-service
GO_SERVICE_BIN_DIR := $(GO_SERVICE_DIR)/bin
KLINE_TASK_CONFIG ?= configs/tasks/kline-default.toml
KLINE_DELETE_TASK_CONFIG ?= configs/tasks/kline-delete-default.toml
export GO111MODULE := on

.PHONY: help
help:
	@echo "Available targets:"
	@echo "  make go-market-data-run"
	@echo "  make go-market-data-admin"
	@echo "  make go-market-data-build"
	@echo "  make go-market-data-clean"
	@echo "  make go-market-data-test"
	@echo "  make go-market-data-tidy"
	@echo "  make go-market-data-check"
	@echo "  make go-strategy-engine-run"
	@echo "  make go-strategy-engine-build"
	@echo "  make go-backtest-engine-run"
	@echo "  make go-backtest-engine-build"
	@echo "  make go-position-engine-run"
	@echo "  make go-position-engine-build"
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
	@echo "  make infra-up          Start Redis, NATS, ClickHouse, and PostgreSQL"
	@echo "  make live-up           Start live market-data stack"
	@echo "  make market-data-up    Start market-data with Docker"
	@echo "  make market-data-down  Stop market-data"
	@echo "  make market-data-logs  Tail market-data logs"
	@echo "  make queue-status      Print NATS JetStream queue lag"
	@echo "  make market-health     Print Redis market health and NATS queue lag"
	@echo "  make kline-check       Run kline integrity check with ClickHouse only"
	@echo "  make kline-backfill    Run kline backfill with ClickHouse only"
	@echo "  make kline-delete-dryrun  Preview kline delete range with ClickHouse only"
	@echo "  make kline-delete-confirm Submit kline delete mutation with ClickHouse only"
	@echo "  make stack-up          Start Redis, NATS, ClickHouse, PostgreSQL, and market-data"
	@echo "  make stack-down        Stop all Docker Compose profile services"
	@echo "  make check            Run all available checks"

.PHONY: go-market-data-run
go-market-data-run:
	cd $(GO_SERVICE_DIR) && go run ./market-data/cmd/market-data -config configs/market-data.local.toml

.PHONY: go-market-data-admin
go-market-data-admin:
	cd $(GO_SERVICE_DIR) && go run ./market-data/cmd/market-data-admin --config configs/market-data.local.toml $(ARGS)

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
	cd $(GO_SERVICE_DIR) && go run ./strategy-engine/cmd/strategy-engine -config configs/strategy-engine.local.toml

.PHONY: go-strategy-engine-build
go-strategy-engine-build:
	mkdir -p $(GO_SERVICE_BIN_DIR)
	cd $(GO_SERVICE_DIR) && go build -o bin/strategy-engine ./strategy-engine/cmd/strategy-engine

.PHONY: go-backtest-engine-run
go-backtest-engine-run:
	cd $(GO_SERVICE_DIR) && go run ./backtest-engine/cmd/backtest-engine -config configs/backtest-engine.local.toml

.PHONY: go-backtest-engine-build
go-backtest-engine-build:
	mkdir -p $(GO_SERVICE_BIN_DIR)
	cd $(GO_SERVICE_DIR) && go build -o bin/backtest-engine ./backtest-engine/cmd/backtest-engine

.PHONY: go-position-engine-run
go-position-engine-run:
	cd $(GO_SERVICE_DIR) && go run ./position-engine/cmd/position-engine -config configs/position-engine.local.toml

.PHONY: go-position-engine-build
go-position-engine-build:
	mkdir -p $(GO_SERVICE_BIN_DIR)
	cd $(GO_SERVICE_DIR) && go build -o bin/position-engine ./position-engine/cmd/position-engine

.PHONY: redis-up
redis-up:
	docker compose --profile infra up -d redis

.PHONY: redis-down
redis-down:
	docker compose stop redis

.PHONY: redis-logs
redis-logs:
	docker compose logs -f redis

.PHONY: redis-cli
redis-cli:
	docker compose exec redis redis-cli -p 6380

.PHONY: clickhouse-up
clickhouse-up:
	docker compose --profile infra up -d clickhouse

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
	docker compose --profile infra up -d postgres

.PHONY: postgres-down
postgres-down:
	docker compose stop postgres

.PHONY: postgres-logs
postgres-logs:
	docker compose logs -f postgres

.PHONY: postgres-shell
postgres-shell:
	docker compose exec postgres psql -U alphaflow -d alphaflow

.PHONY: infra-up
infra-up:
	docker compose --profile infra up -d

.PHONY: live-up
live-up:
	docker compose --profile live up -d --build

.PHONY: market-data-up
market-data-up:
	docker compose --profile live up -d --build market-data

.PHONY: market-data-down
market-data-down:
	docker compose stop market-data

.PHONY: market-data-logs
market-data-logs:
	docker compose logs -f market-data

.PHONY: queue-status
queue-status:
	cd $(GO_SERVICE_DIR) && go run ./market-data/cmd/market-data-admin --config configs/market-data.local.toml queue-status

.PHONY: market-health
market-health:
	cd $(GO_SERVICE_DIR) && go run ./market-data/cmd/market-data-admin --config configs/market-data.local.toml market-health $(ARGS)

.PHONY: kline-check
kline-check:
	docker compose --profile jobs run --rm kline-admin check --task-config $(KLINE_TASK_CONFIG) $(ARGS)

.PHONY: kline-backfill
kline-backfill:
	docker compose --profile jobs run --rm kline-admin backfill --task-config $(KLINE_TASK_CONFIG) $(ARGS)

.PHONY: kline-delete-dryrun
kline-delete-dryrun:
	docker compose --profile jobs run --rm kline-admin delete --task-config $(KLINE_DELETE_TASK_CONFIG) $(ARGS)

.PHONY: kline-delete-confirm
kline-delete-confirm:
	docker compose --profile jobs run --rm kline-admin delete --task-config $(KLINE_DELETE_TASK_CONFIG) --confirm $(ARGS)

.PHONY: stack-up
stack-up:
	docker compose --profile infra --profile live up -d --build

.PHONY: stack-down
stack-down:
	docker compose --profile infra --profile live --profile jobs down --remove-orphans

.PHONY: check
check: go-market-data-check
