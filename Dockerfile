# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/relayforge-api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/relayforge-worker ./cmd/worker \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/relayforge-supervisor ./cmd/supervisor

FROM alpine:3.22

RUN apk add --no-cache ca-certificates wget \
    && addgroup -g 65532 -S relayforge \
    && adduser -u 65532 -S -G relayforge -H -s /sbin/nologin relayforge

COPY --from=builder /out/ /usr/local/bin/

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/relayforge-api"]
