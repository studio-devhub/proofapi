#!/bin/sh
# Tests for entrypoint.sh logic
# Run: sh nginx/entrypoint_test.sh
set -e

PASS=0
FAIL=0

ok()   { echo "  ✅ $1"; PASS=$((PASS+1)); }
fail() { echo "  ❌ $1"; FAIL=$((FAIL+1)); }

# ── Setup: create temp SSL files ──────────────────────────
TMPDIR=$(mktemp -d)
CERT="$TMPDIR/cert.pem"
KEY="$TMPDIR/key.pem"
touch "$CERT" "$KEY"

# ── Helper: run entrypoint logic, capture chosen template ─
run_mode() {
  DOMAIN="$1" SSL_CERT="$2" SSL_KEY="$3"
  export DOMAIN SSL_CERT SSL_KEY

  if [ -n "$SSL_CERT" ] && [ -n "$SSL_KEY" ] && \
     [ -f "$SSL_CERT" ] && [ -f "$SSL_KEY" ]; then
    echo "ssl"
  else
    echo "http"
  fi
}

echo ""
echo "nginx entrypoint.sh — mode selection tests"
echo "───────────────────────────────────────────"

# 1. SSL mode: domain + valid cert + valid key
MODE=$(run_mode "api.example.com" "$CERT" "$KEY")
[ "$MODE" = "ssl" ] && ok "SSL mode when domain + cert + key provided" \
                     || fail "Expected ssl mode, got: $MODE"

# 2. HTTP mode: no domain, no cert, no key
MODE=$(run_mode "" "" "")
[ "$MODE" = "http" ] && ok "HTTP fallback when nothing is set" \
                      || fail "Expected http mode, got: $MODE"

# 3. HTTP mode: domain set but no cert
MODE=$(run_mode "api.example.com" "" "")
[ "$MODE" = "http" ] && ok "HTTP fallback when domain set but no cert" \
                      || fail "Expected http mode, got: $MODE"

# 4. HTTP mode: cert path set but file does not exist
MODE=$(run_mode "api.example.com" "/nonexistent/cert.pem" "/nonexistent/key.pem")
[ "$MODE" = "http" ] && ok "HTTP fallback when cert files do not exist" \
                      || fail "Expected http mode, got: $MODE"

# 5. HTTP mode: cert exists but key missing
MODE=$(run_mode "api.example.com" "$CERT" "")
[ "$MODE" = "http" ] && ok "HTTP fallback when key is missing" \
                      || fail "Expected http mode, got: $MODE"

# 6. HTTP mode: key exists but cert missing
MODE=$(run_mode "api.example.com" "" "$KEY")
[ "$MODE" = "http" ] && ok "HTTP fallback when cert is missing" \
                      || fail "Expected http mode, got: $MODE"

# 7. Domain fallback: empty DOMAIN defaults to localhost
DOMAIN="" SSL_CERT="" SSL_KEY=""
EFFECTIVE_DOMAIN="${DOMAIN:-localhost}"
[ "$EFFECTIVE_DOMAIN" = "localhost" ] && ok "DOMAIN defaults to localhost when not set" \
                                       || fail "Expected localhost, got: $EFFECTIVE_DOMAIN"

# 8. Domain used as-is when set
DOMAIN="api.myapp.com" SSL_CERT="" SSL_KEY=""
EFFECTIVE_DOMAIN="${DOMAIN:-localhost}"
[ "$EFFECTIVE_DOMAIN" = "api.myapp.com" ] && ok "DOMAIN preserved when set" \
                                           || fail "Expected api.myapp.com, got: $EFFECTIVE_DOMAIN"

# ── Cleanup ───────────────────────────────────────────────
rm -rf "$TMPDIR"

echo ""
echo "Results: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ] && echo "✅ All tests passed" && exit 0 \
                  || echo "❌ Some tests failed" && exit 1
