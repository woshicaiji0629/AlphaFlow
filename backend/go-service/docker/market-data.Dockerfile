FROM golang:1.24-alpine AS builder-base

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

FROM builder-base AS market-data-builder

RUN go build -o /out/market-data ./market-data/cmd/market-data

FROM alpine:3.21 AS market-data-runtime

WORKDIR /workspace/backend/go-service

COPY --from=market-data-builder /out/market-data /usr/local/bin/market-data
COPY configs/ configs/

CMD ["market-data", "-config", "configs/market-data.local.toml"]

FROM builder-base AS builder

RUN go build -o /out/market-data ./market-data/cmd/market-data
RUN go build -o /out/market-data-admin ./market-data/cmd/market-data-admin
RUN go build -o /out/strategy-engine ./strategy-engine/cmd/strategy-engine
RUN go build -o /out/position-engine ./position-engine/cmd/position-engine

FROM alpine:3.21

WORKDIR /workspace/backend/go-service

COPY --from=builder /out/market-data /usr/local/bin/market-data
COPY --from=builder /out/market-data-admin /usr/local/bin/market-data-admin
COPY --from=builder /out/strategy-engine /usr/local/bin/strategy-engine
COPY --from=builder /out/position-engine /usr/local/bin/position-engine
COPY configs/ configs/

CMD ["market-data", "-config", "configs/market-data.local.toml"]
