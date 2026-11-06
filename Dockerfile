FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -o /out/relayforge-api ./cmd/api \
    && CGO_ENABLED=0 go build -o /out/relayforge-worker ./cmd/worker \
    && CGO_ENABLED=0 go build -o /out/relayforge-supervisor ./cmd/supervisor

FROM alpine:3.22

COPY --from=builder /out/ /usr/local/bin/

ENTRYPOINT ["/usr/local/bin/relayforge-api"]
