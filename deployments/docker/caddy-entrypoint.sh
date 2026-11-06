#!/bin/sh
set -eu

# The Caddy image starts as root only to bind low ports and fix volume
# ownership. Drop to the unprivileged caddy user before running the server.
if [ "$(id -u)" = "0" ]; then
  if ! id caddy >/dev/null 2>&1; then
    adduser -D -H -u 1000 caddy
  fi
  chown -R caddy:caddy /data /config
  exec su-exec caddy caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
fi

exec caddy run --config /etc/caddy/Caddyfile --adapter caddyfile
