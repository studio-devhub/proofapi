.PHONY: up down restart logs test build clean setup help ngrams ssl

include .env
export

# ── Colors ───────────────────────────────────────────────
GREEN  := \033[0;32m
YELLOW := \033[0;33m
RED    := \033[0;31m
NC     := \033[0m

# ── Default ───────────────────────────────────────────────
.DEFAULT_GOAL := help

## help: Show this help message
help:
	@echo ""
	@echo "  ProofAPI — Commands"
	@echo "  ────────────────────────────────────────"
	@grep -E '^## ' Makefile | sed 's/## /  /'
	@echo ""

# ── Setup ─────────────────────────────────────────────────
## setup: First time setup — checks & installs dependencies, then starts the project
setup:
	@echo ""
	@echo "$(GREEN)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo "$(GREEN)  ProofAPI — Setup$(NC)"
	@echo "$(GREEN)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo ""

	@# ── Detect OS ──────────────────────────────────────────
	@OS=$$(uname -s); \
	if [ "$$OS" = "Darwin" ]; then \
		echo "  $(YELLOW)OS: macOS$(NC)"; \
		if ! command -v brew >/dev/null 2>&1; then \
			echo "  $(YELLOW)Installing Homebrew...$(NC)"; \
			/bin/bash -c "$$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"; \
		else \
			echo "  ✅ Homebrew already installed"; \
		fi; \
	elif [ "$$OS" = "Linux" ]; then \
		echo "  $(YELLOW)OS: Linux$(NC)"; \
	else \
		echo "  $(RED)Unsupported OS: $$OS$(NC)"; exit 1; \
	fi

	@# ── Check & Install: curl ──────────────────────────────
	@if ! command -v curl >/dev/null 2>&1; then \
		echo "  $(YELLOW)Installing curl...$(NC)"; \
		OS=$$(uname -s); \
		if [ "$$OS" = "Darwin" ]; then brew install curl; \
		elif command -v apt-get >/dev/null 2>&1; then sudo apt-get install -y curl; \
		elif command -v yum >/dev/null 2>&1; then sudo yum install -y curl; \
		fi; \
	else \
		echo "  ✅ curl $$(curl --version | head -1 | awk '{print $$2}')"; \
	fi

	@# ── Check & Install: unzip ────────────────────────────
	@if ! command -v unzip >/dev/null 2>&1; then \
		echo "  $(YELLOW)Installing unzip...$(NC)"; \
		OS=$$(uname -s); \
		if [ "$$OS" = "Darwin" ]; then brew install unzip; \
		elif command -v apt-get >/dev/null 2>&1; then sudo apt-get install -y unzip; \
		elif command -v yum >/dev/null 2>&1; then sudo yum install -y unzip; \
		fi; \
	else \
		echo "  ✅ unzip"; \
	fi

	@# ── Check & Install: Docker ───────────────────────────
	@if ! command -v docker >/dev/null 2>&1; then \
		echo "  $(YELLOW)Installing Docker...$(NC)"; \
		OS=$$(uname -s); \
		if [ "$$OS" = "Darwin" ]; then \
			brew install --cask docker; \
			echo "  $(YELLOW)Opening Docker Desktop — please start it and press Enter when ready...$(NC)"; \
			open /Applications/Docker.app; \
			read -p "" dummy; \
		elif command -v apt-get >/dev/null 2>&1; then \
			sudo apt-get update && sudo apt-get install -y docker.io; \
			sudo systemctl enable docker && sudo systemctl start docker; \
			sudo usermod -aG docker $$USER; \
			echo "  $(YELLOW)Added $$USER to docker group — you may need to re-login$(NC)"; \
		elif command -v yum >/dev/null 2>&1; then \
			sudo yum install -y docker; \
			sudo systemctl enable docker && sudo systemctl start docker; \
			sudo usermod -aG docker $$USER; \
		fi; \
	else \
		echo "  ✅ Docker $$(docker --version | awk '{print $$3}' | tr -d ',')"; \
	fi

	@# ── Check & Install: Docker Compose ──────────────────
	@if ! docker compose version >/dev/null 2>&1; then \
		if ! command -v docker-compose >/dev/null 2>&1; then \
			echo "  $(YELLOW)Installing Docker Compose plugin...$(NC)"; \
			OS=$$(uname -s); \
			if [ "$$OS" = "Darwin" ]; then \
				brew install docker-compose; \
			elif command -v apt-get >/dev/null 2>&1; then \
				sudo apt-get install -y docker-compose-plugin; \
			else \
				ARCH=$$(uname -m); \
				sudo curl -SL "https://github.com/docker/compose/releases/latest/download/docker-compose-linux-$$ARCH" \
					-o /usr/local/lib/docker/cli-plugins/docker-compose; \
				sudo chmod +x /usr/local/lib/docker/cli-plugins/docker-compose; \
			fi; \
		fi; \
	else \
		echo "  ✅ Docker Compose $$(docker compose version --short 2>/dev/null || echo 'v2')"; \
	fi

	@# ── Verify Docker is running ──────────────────────────
	@if ! docker info >/dev/null 2>&1; then \
		echo ""; \
		echo "  $(RED)Docker daemon is not running!$(NC)"; \
		OS=$$(uname -s); \
		if [ "$$OS" = "Darwin" ]; then \
			echo "  $(YELLOW)Starting Docker Desktop...$(NC)"; \
			open /Applications/Docker.app; \
			echo "  Waiting for Docker to start"; \
			for i in $$(seq 1 30); do \
				docker info >/dev/null 2>&1 && break; \
				printf "."; sleep 2; \
			done; \
			echo ""; \
		else \
			echo "  $(YELLOW)Run: sudo systemctl start docker$(NC)"; exit 1; \
		fi; \
	fi
	@docker info >/dev/null 2>&1 && echo "  ✅ Docker daemon running" || (echo "  $(RED)Docker still not running — start Docker and retry$(NC)"; exit 1)

	@# ── .env setup ────────────────────────────────────────
	@echo ""
	@if [ ! -f .env ]; then \
		echo "  $(YELLOW)Generating .env with secure random secrets...$(NC)"; \
		API_SECRET=$$(openssl rand -hex 32); \
		REDIS_SECRET=$$(openssl rand -hex 16); \
		printf "PORT=4003\nAPI_KEY=$$API_SECRET\nREDIS_PASSWORD=$$REDIS_SECRET\n" > .env; \
		echo "  ✅ .env created with auto-generated secrets"; \
	else \
		echo "  ✅ .env exists"; \
	fi

	@# ── Create required directories ───────────────────────
	@mkdir -p ngrams nginx
	@echo "  ✅ Directories ready (ngrams/, nginx/)"

	@# ── Done ──────────────────────────────────────────────
	@echo ""
	@echo "$(GREEN)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo "$(GREEN)  All dependencies satisfied!$(NC)"
	@echo "$(GREEN)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(NC)"
	@echo ""
	@echo "  Optional: Download NGrams for better accuracy (~8GB):"
	@echo "    make ngrams"
	@echo ""
	@echo "  $(GREEN)Starting services now...$(NC)"
	@echo ""
	@$(MAKE) up

# ── Start ─────────────────────────────────────────────────
## up: Start all services (LanguageTool + Redis + API)
up:
	@echo "$(GREEN)Starting all services...$(NC)"
	@docker compose up -d --build
	@echo ""
	@echo "$(GREEN)✅ Services started!$(NC)"
	@echo ""
	@echo "  API:          http://localhost:$(PORT)"
	@echo "  Health check: http://localhost:$(PORT)/v1/health"
	@echo ""
	@echo "  Waiting for LanguageTool to be ready (can take ~60s)..."
	@sleep 5
	@docker compose ps

## up-prod: Start with Nginx reverse proxy (HTTP or HTTPS based on .env)
up-prod:
	@echo "$(GREEN)Starting production stack...$(NC)"
	@if [ -n "$(DOMAIN)" ]; then \
		echo "  Domain:  $(DOMAIN)"; \
	else \
		echo "  $(YELLOW)DOMAIN not set — falling back to localhost$(NC)"; \
	fi
	@if [ -n "$(SSL_CERT)" ] && [ -f "$(SSL_CERT)" ]; then \
		echo "  SSL:     $(GREEN)enabled$(NC)"; \
	else \
		echo "  $(YELLOW)SSL_CERT not set or file missing — running HTTP only$(NC)"; \
	fi
	@docker compose --profile production up -d --build
	@docker compose ps

## down: Stop all services
down:
	@echo "$(RED)Stopping all services...$(NC)"
	@docker compose down
	@echo "$(GREEN)✅ Stopped$(NC)"

## down-clean: Stop and remove volumes (WARNING: deletes Redis data)
down-clean:
	@echo "$(RED)Stopping and removing all data...$(NC)"
	@docker compose down -v
	@echo "$(GREEN)✅ Cleaned$(NC)"

## restart: Restart all services
restart: down up

## restart-api: Rebuild and restart only the API
restart-api:
	@echo "$(YELLOW)Restarting API...$(NC)"
	@docker compose up -d --build api
	@echo "$(GREEN)✅ API restarted$(NC)"

# ── Logs ──────────────────────────────────────────────────
## logs: Follow all logs
logs:
	@docker compose logs -f

## logs-api: Follow API logs only
logs-api:
	@docker compose logs -f api

## logs-lt: Follow LanguageTool logs only
logs-lt:
	@docker compose logs -f languagetool

# ── Test ──────────────────────────────────────────────────
## test: Run all tests
test:
	@echo "$(GREEN)Running tests...$(NC)"
	@GONOSUMDB="*" GOFLAGS="-mod=mod" GOPROXY="direct" \
		go test ./... -v -coverprofile=coverage.out
	@go tool cover -func=coverage.out | tail -1
	@echo "$(GREEN)✅ Tests complete$(NC)"

## test-docker: Run tests inside Docker
test-docker:
	@echo "$(GREEN)Running tests in Docker...$(NC)"
	@docker compose -f docker-compose.test.yml up --build --abort-on-container-exit
	@docker compose -f docker-compose.test.yml down

## test-spell: Run spelling suggestion tests only
test-spell:
	@GONOSUMDB="*" GOFLAGS="-mod=mod" GOPROXY="direct" \
		go test ./internal/languagetool/... -run TestSpelling -v

## test-latency: Run latency tests only
test-latency:
	@GONOSUMDB="*" GOFLAGS="-mod=mod" GOPROXY="direct" \
		go test ./internal/languagetool/... -run TestLatency -v

## cover: Open test coverage report in browser
cover:
	@GONOSUMDB="*" GOFLAGS="-mod=mod" go test ./... -coverprofile=coverage.out
	@go tool cover -html=coverage.out

# ── Build ─────────────────────────────────────────────────
## build: Build Docker image only
build:
	@echo "$(GREEN)Building Docker image...$(NC)"
	@docker compose build api
	@echo "$(GREEN)✅ Build complete$(NC)"

## build-local: Build binary locally
build-local:
	@echo "$(GREEN)Building binary...$(NC)"
	@GONOSUMDB="*" GOFLAGS="-mod=mod" GOPROXY="direct" \
		CGO_ENABLED=0 go build -o ./api ./cmd/api
	@echo "$(GREEN)✅ Binary: ./api$(NC)"

# ── Health & Debug ────────────────────────────────────────
## health: Check all service health
health:
	@echo "$(GREEN)Checking health...$(NC)"
	@echo ""
	@echo "  API:"
	@curl -s http://localhost:$(PORT)/v1/health \
		-H "X-API-Key: $(API_KEY)" | python3 -m json.tool 2>/dev/null || \
		curl -s http://localhost:$(PORT)/v1/health | cat
	@echo ""

## status: Show container status
status:
	@docker compose ps

## redis-cli: Open Redis CLI
redis-cli:
	@docker exec -it lt-redis redis-cli \
		-a "$$(grep REDIS_PASSWORD .env | cut -d= -f2)"

## redis-stats: Show Redis cache stats
redis-stats:
	@docker exec lt-redis redis-cli \
		-a "$$(grep REDIS_PASSWORD .env | cut -d= -f2)" \
		info stats | grep -E "hits|misses|keys"

# ── NGrams (optional, improves accuracy) ─────────────────
## ngrams: Download English NGrams (~8GB, improves accuracy)
ngrams:
	@echo "$(YELLOW)Downloading English NGrams (~8GB)...$(NC)"
	@mkdir -p ngrams
	@curl -L -C - "https://languagetool.org/download/ngram-data/ngrams-en-20150817.zip" \
		-o /tmp/ngrams-en.zip
	@unzip /tmp/ngrams-en.zip -d ngrams/
	@echo "$(GREEN)✅ NGrams ready. Restart: make restart$(NC)"

# ── Quick API Test ────────────────────────────────────────
## curl-test: Quick API test with curl
curl-test:
	@echo "$(GREEN)Testing API...$(NC)"
	@curl -s -X POST http://localhost:$(PORT)/v1/check \
		-H "Content-Type: application/json" \
		-H "X-API-Key: $(API_KEY)" \
		-d '{"text":"I recieve wierd emails definately","language":"en-US"}' \
		| python3 -m json.tool 2>/dev/null || \
	curl -s -X POST http://localhost:$(PORT)/v1/check \
		-H "Content-Type: application/json" \
		-H "X-API-Key: $(API_KEY)" \
		-d '{"text":"I recieve wierd emails definately","language":"en-US"}'
	@echo ""

## clean: Remove build artifacts
clean:
	@rm -f api coverage.out
	@echo "$(GREEN)✅ Cleaned$(NC)"
