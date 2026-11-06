# syntax=docker/dockerfile:1

# Builds the dashboard and serves it with Caddy, which also terminates TLS and
# proxies /api to the RelayForge API container.

FROM node:22-alpine AS builder

WORKDIR /app
RUN corepack enable

COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY web ./
RUN pnpm build

FROM caddy:2-alpine

RUN apk add --no-cache su-exec

COPY --from=builder /app/dist /srv
COPY deployments/docker/Caddyfile /etc/caddy/Caddyfile
COPY deployments/docker/caddy-entrypoint.sh /usr/local/bin/caddy-entrypoint.sh
RUN chmod +x /usr/local/bin/caddy-entrypoint.sh

EXPOSE 8080 8443

ENTRYPOINT ["/usr/local/bin/caddy-entrypoint.sh"]
