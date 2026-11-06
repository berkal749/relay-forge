FROM golang:1.26-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

ARG TARGET=api
RUN CGO_ENABLED=0 go build -o /out/relayforge ./cmd/${TARGET}

FROM alpine:3.22

COPY --from=builder /out/relayforge /usr/local/bin/relayforge

ENTRYPOINT ["relayforge"]
