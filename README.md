# ProofAPI

A high-performance, production-ready grammar and spell-checking API built with Go. Wraps the open-source [LanguageTool](https://languagetool.org) engine and exposes a clean REST + WebSocket API with Redis caching, API key authentication, and rate limiting — all orchestrated via Docker Compose.

---

## Features

- **REST API** — single endpoint to check grammar and spelling
- **WebSocket API** — real-time checking with debounce (ideal for editors and live input)
- **Redis caching** — identical requests served in <1ms after first check
- **API key authentication** — all endpoints protected
- **Rate limiting** — per-IP request throttling with automatic cleanup
- **NGram support** — optional 8GB language model for significantly improved accuracy
- **50+ languages** — English, French, German, Spanish, Arabic, Chinese, and more
- **Health endpoint** — real-time status of all services
- **One-command setup** — `make setup` installs all dependencies and starts the stack

---

## Tech Stack

| Layer | Technology |
| ----- | ---------- |
| Language | [Go 1.22](https://go.dev) |
| HTTP Router | [go-chi/chi v5](https://github.com/go-chi/chi) |
| WebSocket | [gorilla/websocket](https://github.com/gorilla/websocket) |
| Cache | [Redis 7](https://redis.io) via [go-redis/v9](https://github.com/redis/go-redis) |
| Grammar Engine | [LanguageTool](https://languagetool.org) (erikvl87/languagetool Docker image) |
| Reverse Proxy | [Nginx](https://nginx.org) (optional, production profile) |
| Containerization | [Docker](https://docker.com) + [Docker Compose](https://docs.docker.com/compose/) |
| Testing | [testify](https://github.com/stretchr/testify), [miniredis](https://github.com/alicebob/miniredis) |

---

## Architecture

```text
Client
  │
  ├── REST  ──► POST /v1/check
  │
  └── WS   ──► GET  /v1/ws
                      │
              ┌───────▼────────┐
              │   Go API (chi) │
              │  Auth + Rate   │
              │    Limiting    │
              └───┬───────┬───┘
                  │       │
           ┌──────▼──┐ ┌──▼──────────┐
           │  Redis  │ │LanguageTool │
           │  Cache  │ │   Engine    │
           └─────────┘ └────────────┘
```

**Request flow:**

1. Request hits Go API → API key validated → rate limit checked
2. Cache key computed from `(language + level + text)` SHA-256 hash
3. Redis hit → return immediately with `"cached": true`
4. Redis miss → forward to LanguageTool engine → store result in Redis → return

---

## Quick Start

### Prerequisites

`make setup` handles everything automatically. It installs:

- Homebrew (macOS)
- curl, unzip
- Docker + Docker Compose
- Generates `.env` with secure random secrets

```bash
git clone <repo-url>
cd languagetool-backend
make setup
```

That's it. The API will be available at `http://localhost:4003`.

> **Note:** LanguageTool takes ~60 seconds to fully start on first launch. Use `make health` to check readiness.

---

## Environment Variables

| Variable | Default | Description |
| -------- | ------- | ----------- |
| `PORT` | `4003` | API server port |
| `API_KEY` | *(required)* | Secret key for all authenticated endpoints |
| `REDIS_PASSWORD` | *(required)* | Redis auth password |
| `REDIS_HOST` | `redis` | Redis hostname |
| `REDIS_PORT` | `6379` | Redis port |
| `LT_URL` | `http://languagetool:8010` | LanguageTool base URL |
| `ALLOWED_ORIGINS` | *(empty = all)* | Comma-separated allowed WebSocket origins |

---

## API Reference

### Authentication

All endpoints except `/v1/health` require the `X-API-Key` header.

```http
X-API-Key: your-api-key
```

WebSocket also accepts `?api_key=your-api-key` as a query parameter.

---

### REST Endpoints

#### `POST /v1/check`

Check text for grammar and spelling errors.

#### Request

```json
{
  "text": "I recieve wierd emails definately",
  "language": "en-US",
  "level": "default"
}
```

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `text` | string | Yes | Text to check (2–20,000 characters) |
| `language` | string | No | BCP 47 language code. Default: `en-US` |
| `level` | string | No | `default` or `picky`. Default: `default` |

**Response `200 OK`**

```json
{
  "matches": [
    {
      "message": "Possible spelling mistake found.",
      "offset": 2,
      "length": 7,
      "replacements": [
        { "value": "receive" },
        { "value": "relieve" }
      ],
      "rule": {
        "id": "MORFOLOGIK_RULE_EN_US",
        "description": "Possible spelling mistake",
        "issueType": "misspelling",
        "category": {
          "id": "TYPOS",
          "name": "Possible Typo"
        }
      },
      "context": {
        "text": "I recieve wierd emails definately",
        "offset": 2,
        "length": 7
      }
    }
  ],
  "language": {
    "name": "English (US)",
    "code": "en-US"
  },
  "checkedAt": "2026-05-18T07:06:08Z",
  "cached": false
}
```

**Cached Response** — second identical request returns:

```json
{
  "cached": true,
  "cacheExpiresIn": 298
}
```

---

#### `GET /v1/languages`

Returns all supported languages.

**Response `200 OK`**

```json
[
  { "code": "en", "longCode": "en-US", "name": "English (US)" },
  { "code": "de", "longCode": "de-DE", "name": "German (Germany)" }
]
```

---

#### `DELETE /v1/cache`

Clears all cached check results from Redis.

**Response `200 OK`**

```json
{ "deleted": 42 }
```

---

#### `GET /v1/health`

Health check — no authentication required.

**Response `200 OK`**

```json
{
  "api": "ok",
  "languagetool": "ok",
  "redis": "ok",
  "websocket": {
    "active": 3,
    "total": 47
  },
  "cacheStats": {
    "hits": 1024,
    "misses": 83,
    "keys": 210,
    "memoryUsed": "4.21M"
  }
}
```

Returns `503 Service Unavailable` if LanguageTool or Redis is unreachable.

---

### WebSocket API

Connect to `/v1/ws` for real-time checking with debounce. Ideal for editor integrations where text is checked as the user types.

```text
ws://localhost:4003/v1/ws?api_key=your-api-key
```

#### Connection

On connect, the server immediately sends an acknowledgement:

```json
{
  "type": "ack",
  "payload": {
    "connId": "1716019568123456789",
    "status": "connected"
  }
}
```

#### Message Types

| Type | Direction | Description |
| ---- | --------- | ----------- |
| `check` | Client → Server | Submit text for checking |
| `result` | Server → Client | Grammar/spelling results |
| `error` | Server → Client | Error response |
| `ping` | Client → Server | Keepalive ping |
| `pong` | Server → Client | Keepalive response |
| `ack` | Server → Client | Connection acknowledged |

#### Send a Check Request

```json
{
  "type": "check",
  "text": "I recieve wierd emails",
  "language": "en-US",
  "seqId": 1
}
```

| Field | Type | Description |
| ----- | ---- | ----------- |
| `type` | string | Must be `"check"` |
| `text` | string | Text to check (2–20,000 chars) |
| `language` | string | BCP 47 code. Default: `en-US` |
| `seqId` | int | Client-assigned sequence number for ordering responses |

> **Debounce:** The server waits 150ms after the last received message before processing. If another message arrives within that window, the timer resets. This prevents unnecessary checks on rapid keystrokes.

#### Receive a Result

```json
{
  "type": "result",
  "seqId": 1,
  "payload": {
    "matches": [
      {
        "message": "Possible spelling mistake found.",
        "offset": 2,
        "length": 7,
        "replacements": [{ "value": "receive" }],
        "rule": {
          "id": "MORFOLOGIK_RULE_EN_US",
          "description": "Possible spelling mistake",
          "issueType": "misspelling",
          "category": { "id": "TYPOS", "name": "Possible Typo" }
        },
        "context": {
          "text": "I recieve wierd emails",
          "offset": 2,
          "length": 7
        }
      }
    ],
    "language": { "name": "English (US)", "code": "en-US" },
    "cached": false,
    "latencyMs": 43
  }
}
```

#### Keepalive

Send a ping every 30 seconds to keep the connection alive:

```json
{ "type": "ping" }
```

Server responds:

```json
{ "type": "pong" }
```

> The server automatically sends WebSocket-level ping frames every 30 seconds. Connections with no pong response within 60 seconds are closed.

#### JavaScript Example

```javascript
const ws = new WebSocket('ws://localhost:4003/v1/ws?api_key=your-api-key');
let seq = 0;

ws.onopen = () => console.log('Connected');

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === 'result') {
    console.log('Matches:', msg.payload.matches);
    console.log('Latency:', msg.payload.latencyMs + 'ms');
    console.log('Cached:', msg.payload.cached);
  }
};

function check(text, language = 'en-US') {
  ws.send(JSON.stringify({
    type: 'check',
    text,
    language,
    seqId: ++seq
  }));
}

check('I recieve wierd emails definately');
```

---

## Make Commands

```bash
make setup        # First-time setup: install deps, generate secrets, start services
make up           # Start all services
make down         # Stop all services
make restart      # Restart all services
make restart-api  # Rebuild and restart only the Go API
make logs         # Follow all logs
make logs-api     # Follow API logs only
make logs-lt      # Follow LanguageTool logs only
make health       # Check all service health
make status       # Show container status
make test         # Run unit tests
make test-docker  # Run tests inside Docker
make build        # Build Docker image
make ngrams       # Download English NGrams (~8GB, improves accuracy)
make redis-cli    # Open Redis CLI
make redis-stats  # Show Redis cache hit/miss stats
make clean        # Remove build artifacts
```

---

## NGrams (Optional)

NGrams significantly improve accuracy for confusable words (e.g. *their* vs *there*, *its* vs *it's*).

```bash
make ngrams
make restart
```

> **Warning:** Requires ~8GB disk space and ~1GB additional RAM for LanguageTool.

---

## Production Deployment

### 1. Get an SSL certificate

Obtain a certificate from any provider — Cloudflare, AWS ACM, ZeroSSL, or your own CA. You need two files:

- **Certificate** (`.crt` or `fullchain.pem`)
- **Private key** (`.key` or `privkey.pem`)

Copy them anywhere on your server, e.g.:

```bash
/etc/ssl/proofapi/fullchain.pem
/etc/ssl/proofapi/privkey.pem
```

### 2. Set domain and cert paths in `.env`

```env
DOMAIN=api.yourdomain.com
SSL_CERT=/etc/ssl/proofapi/fullchain.pem
SSL_KEY=/etc/ssl/proofapi/privkey.pem
ALLOWED_ORIGINS=https://yourapp.com
```

### 3. Start the production stack

```bash
make up-prod
```

`make up-prod` validates all three values exist before starting. Nginx reads `nginx.conf.template` at startup and injects `DOMAIN`, `SSL_CERT`, and `SSL_KEY` automatically — no manual nginx.conf editing needed.

Starts the full stack with Nginx on ports 80 + 443:

- HTTP → HTTPS redirect (port 80)
- TLS 1.2/1.3 with your certificate
- WebSocket proxying (`wss://`) on `/v1/ws`
- Rate limiting (60 requests/minute per IP)
- Security headers (`X-Frame-Options`, `HSTS`, `X-Content-Type-Options`)

### Configure WebSocket CORS

Set `ALLOWED_ORIGINS` in `.env` to restrict WebSocket connections:

```env
ALLOWED_ORIGINS=https://yourapp.com,https://app.yourapp.com
```

Leave empty to allow all origins (development only).

### Docker Compose Services

| Service | Container | Port | Description |
| ------- | --------- | ---- | ----------- |
| `languagetool` | `lt-engine` | 8010 (internal) | LanguageTool grammar engine |
| `redis` | `lt-redis` | 6379 (internal) | Cache layer |
| `api` | `lt-api` | 4003 | Go REST + WebSocket API |
| `nginx` | `lt-nginx` | 80 | Reverse proxy (production profile only) |

---

## Testing

```bash
make test
```

Runs the full test suite including:

- Unit tests for all packages
- Middleware tests (API key auth, rate limiting)
- Handler tests with miniredis (in-memory Redis mock)
- WebSocket connection and debounce tests
- LanguageTool client edge cases
- Latency benchmarks

```bash
make test-docker   # Run tests in isolated Docker environment
make cover         # Open HTML coverage report in browser
```

---

## Response Codes

| Code | Description |
| ---- | ----------- |
| `200` | Success |
| `400` | Bad request (invalid body, text too short/long) |
| `401` | Missing or invalid API key |
| `429` | Rate limit exceeded |
| `503` | LanguageTool or Redis unavailable |

---

## License

MIT
