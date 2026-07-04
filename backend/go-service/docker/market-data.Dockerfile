FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /out/market-data ./market-data/cmd/market-data
RUN go build -o /out/market-data-admin ./market-data/cmd/market-data-admin

FROM alpine:3.21

WORKDIR /workspace/backend/go-service

COPY --from=builder /out/market-data /usr/local/bin/market-data
COPY --from=builder /out/market-data-admin /usr/local/bin/market-data-admin
COPY configs/ configs/

CMD ["market-data", "-config", "configs/market-data.local.toml"]
