# ProofAPI

A high-performance, production-ready spell-checking API built with Go. Wraps the open-source [LanguageTool](https://languagetool.org) engine and exposes a clean REST + WebSocket API with Redis caching, API key authentication, rate limiting, and Swagger UI — all orchestrated via Docker Compose.

---

## Features

- **REST API** — spell checking in one request
- **WebSocket API** — real-time spell checking as the user types (150ms server-side debounce)
- **Redis caching** — identical requests served in <1ms after first check
- **Swagger UI** — interactive API docs at `/docs/index.html`
- **API key authentication** — all endpoints protected
- **Rate limiting** — per-IP request throttling with automatic cleanup
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
| Grammar Engine | [LanguageTool](https://languagetool.org) (erikvl87/languagetool) |
| API Docs | [swaggo/swag](https://github.com/swaggo/swag) — OpenAPI 3.0 + Swagger UI |
| Reverse Proxy | [Nginx](https://nginx.org) (optional, production profile) |
| Containerization | [Docker](https://docker.com) + [Docker Compose](https://docs.docker.com/compose/) |
| Testing | [testify](https://github.com/stretchr/testify), [miniredis](https://github.com/alicebob/miniredis) |

---

## Architecture

```text
Client
  │
  ├── REST  ──► POST /v1/check
  ├── WS    ──► GET  /v1/ws
  └── Docs  ──► GET  /docs
                      │
              ┌───────▼────────┐
              │   Go API (chi) │
              │  Auth + Rate   │
              │    Limiting    │
              └───┬───────┬───┘
                  │       │
           ┌──────▼──┐ ┌──▼──────────┐
           │  Redis  │ │LanguageTool │
           │  Cache  │ │ LanguageTool│
           └─────────┘ └────────────┘
```

**Request flow:**

1. Request hits Go API → API key validated → rate limit checked
2. Cache key computed from `SHA-256(language + level + categories + text)`
3. Redis hit → return immediately with `"cached": true`
4. Redis miss → forward to LanguageTool engine → store result in Redis → return

---

## Quick Start

```bash
git clone https://github.com/studio-devhub/proofapi
cd proofapi
make setup
```

`make setup` handles everything automatically — installs Docker, generates `.env` with secure random secrets, and starts all services.

> **Note:** LanguageTool takes ~60 seconds to fully start on first launch. Use `make health` to check readiness.

| URL | Description |
| --- | ----------- |
| `http://localhost:4003/v1/health` | Health check |
| `http://localhost:4003/docs/index.html` | Swagger UI |
| `ws://localhost:4003/v1/ws` | WebSocket endpoint |

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
| `DOMAIN` | `localhost` | Production domain for Nginx |
| `SSL_CERT` | *(optional)* | Path to TLS certificate file |
| `SSL_KEY` | *(optional)* | Path to TLS private key file |

---

## API Reference

### Authentication

All endpoints except `/v1/health` and `/docs` require the `X-API-Key` header.

```http
X-API-Key: your-api-key
```

WebSocket also accepts `?api_key=your-api-key` as a query parameter.

---

### `POST /v1/check`

Check text for spelling errors.

**Request**

```json
{
  "text": "I recieve wierd emails definately",
  "language": "en-US"
}
```

| Field | Type | Required | Description |
| ----- | ---- | -------- | ----------- |
| `text` | string | ✅ | Text to check (2–20,000 characters) |
| `language` | string | | BCP 47 language code. Default: `en-US` |
| `clientId` | string | | Optional client identifier for custom dictionary filtering |

**Response `200 OK`**

```json
{
  "matches": [
    {
      "message": "Possible spelling mistake found.",
      "offset": 2,
      "length": 7,
      "replacements": [
        { "value": "receive" }
      ],
      "rule": {
        "id": "MORFOLOGIK_RULE_EN_US",
        "description": "Possible spelling mistake",
        "issueType": "misspelling",
        "category": { "id": "TYPOS", "name": "Possible Typo" }
      },
      "context": {
        "text": "I recieve wierd emails definately",
        "offset": 2,
        "length": 7
      }
    }
  ],
  "language": { "name": "English (US)", "code": "en-US" },
  "checkedAt": "2026-05-18T07:06:08Z",
  "cached": false
}
```

---

### `GET /v1/languages`

Returns all supported languages. Results cached for 1 hour.

```json
[
  { "code": "en", "longCode": "en-US", "name": "English (US)" },
  { "code": "de", "longCode": "de-DE", "name": "German (Germany)" }
]
```

---

### `DELETE /v1/cache`

Clears all cached grammar check results from Redis.

```json
{ "deleted": 42 }
```

---

### `GET /v1/health`

No authentication required.

```json
{
  "api": "ok",
  "languagetool": "ok",
  "redis": "ok",
  "websocket": { "active": 3, "total": 47 },
  "cacheStats": {
    "hits": 1024,
    "misses": 83,
    "keys": 210,
    "memoryUsed": "4.21M"
  }
}
```

Returns `503` if LanguageTool or Redis is unreachable.

---

### `GET /docs/index.html`

Swagger UI — interactive API explorer. No authentication required.

---

### WebSocket API

Connect to `/v1/ws` for real-time checking. The server debounces 150ms — no need to debounce on the client.

```
ws://localhost:4003/v1/ws?api_key=your-api-key
```

#### Message Types

| Type | Direction | Description |
| ---- | --------- | ----------- |
| `check` | Client → Server | Submit text for checking |
| `result` | Server → Client | Grammar/spelling results |
| `error` | Server → Client | Error response |
| `ping` | Client → Server | Keepalive |
| `pong` | Server → Client | Keepalive response |
| `ack` | Server → Client | Connection acknowledged |

#### Send a Check

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
| `type` | string | `"check"` |
| `text` | string | Text to check (2–20,000 chars) |
| `language` | string | BCP 47 code. Default: `en-US` |
| `clientId` | string | Optional client identifier for custom dictionary filtering |
| `seqId` | int | Client sequence number for ordering |

#### Receive a Result

```json
{
  "type": "result",
  "seqId": 1,
  "payload": {
    "matches": [...],
    "language": { "name": "English (US)", "code": "en-US" },
    "cached": false,
    "latencyMs": 43
  }
}
```

#### Keepalive

```json
{ "type": "ping" }
```

Server responds with `{ "type": "pong" }`. The server also sends WebSocket-level ping frames every 30 seconds — connections with no pong within 60 seconds are closed.

#### JavaScript Example

```javascript
const ws = new WebSocket('ws://localhost:4003/v1/ws?api_key=your-api-key');
let seq = 0;

ws.onopen = () => console.log('Connected');

ws.onmessage = (event) => {
  const msg = JSON.parse(event.data);
  if (msg.type === 'result') {
    console.log(`${msg.payload.matches.length} issues found (${msg.payload.latencyMs}ms)`);
    console.log('Cached:', msg.payload.cached);
  }
};

function check(text, language = 'en-US') {
  ws.send(JSON.stringify({
    type: 'check',
    text,
    language,
    seqId: ++seq,
  }));
}

check('I recieve wierd emails definately');
```

---

## Make Commands

```bash
make setup        # First-time setup: install deps, generate secrets, start services
make up           # Start all services
make up-prod      # Start with Nginx reverse proxy (HTTP or HTTPS based on .env)
make down         # Stop all services
make down-clean   # Stop and remove volumes (WARNING: deletes Redis data)
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
make swagger      # Regenerate Swagger docs from annotations
make redis-cli    # Open Redis CLI
make redis-stats  # Show Redis cache hit/miss stats
make clean        # Remove build artifacts
```

---

## Swagger UI

Interactive API documentation is available at:

```
http://localhost:4003/docs/index.html
```

To regenerate docs after changing handler annotations:

```bash
make swagger
```

---

## Production Deployment

### Recommended: AWS `t3.large`

| Service | RAM usage |
| ------- | --------- |
| LanguageTool | ~1.5–2 GB |
| Redis | ~150 MB |
| Go API | ~50 MB |
| OS buffer | ~500 MB |

`t3.large` (8GB RAM) gives comfortable headroom. Use Spot for ~$18–22/mo.

### 1. Get an SSL certificate

Copy your certificate files to the server:

```bash
/etc/ssl/proofapi/fullchain.pem
/etc/ssl/proofapi/privkey.pem
```

### 2. Configure `.env`

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

> **Upgrading from a previous version?** The cache key format changed (removed `level`/`enabledCategories` components). Flush stale Redis entries after deploy to avoid memory waste:
> ```bash
> curl -X DELETE https://api.yourdomain.com/v1/cache -H "X-API-Key: your-api-key"
> ```

Nginx starts on ports 80 + 443 with:
- HTTP → HTTPS redirect
- TLS 1.2/1.3
- WebSocket proxying (`wss://`) on `/v1/ws`
- Security headers (HSTS, X-Frame-Options, X-Content-Type-Options)

> If `SSL_CERT`/`SSL_KEY` are not set, Nginx falls back to HTTP automatically.

---

## Testing

```bash
make test          # Unit tests for all packages
make test-docker   # Run tests in isolated Docker environment
make cover         # Open HTML coverage report in browser
```

Test coverage includes:
- Middleware (API key auth, rate limiting, JSON error responses)
- Handler tests with miniredis
- WebSocket connection, debounce, ping/pong, CORS
- LanguageTool client edge cases and latency benchmarks
- Cache stats parsing
- Nginx entrypoint SSL/HTTP mode selection (8 shell tests)

---

## Response Codes

| Code | Description |
| ---- | ----------- |
| `200` | Success |
| `400` | Invalid body, text too short/long |
| `401` | Missing or invalid API key |
| `429` | Rate limit exceeded |
| `503` | LanguageTool or Redis unavailable |

---

## Frontend Integration

See [FRONTEND_INTEGRATION.md](FRONTEND_INTEGRATION.md) for a complete React (Vite) integration guide covering REST, WebSocket, TipTap rich text editor, and inline error highlighting.

A runnable example is in [`examples/react/`](examples/react/):

```bash
cd examples/react
cp .env.example .env   # add your API key
yarn install
yarn dev               # http://localhost:5173
```

---

## License

MIT
