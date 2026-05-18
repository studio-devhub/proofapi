#!/bin/sh
set -e

DOMAIN="${DOMAIN:-localhost}"
SSL_CERT="${SSL_CERT:-}"
SSL_KEY="${SSL_KEY:-}"

# ── Decide mode: HTTPS or HTTP-only ──────────────────────
if [ -n "$SSL_CERT" ] && [ -n "$SSL_KEY" ] && \
   [ -f "$SSL_CERT" ] && [ -f "$SSL_KEY" ]; then
  echo "[nginx] SSL mode — domain: $DOMAIN"
  TEMPLATE=/etc/nginx/templates/nginx-ssl.conf.template
else
  echo "[nginx] HTTP-only mode — domain: $DOMAIN (no SSL cert provided)"
  TEMPLATE=/etc/nginx/templates/nginx-http.conf.template
fi

# Inject env vars into chosen template
envsubst '${DOMAIN} ${SSL_CERT} ${SSL_KEY}' \
  < "$TEMPLATE" > /etc/nginx/nginx.conf

exec nginx -g "daemon off;"
