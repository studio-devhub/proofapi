// Package main is the entry point for ProofAPI.
//
//	@title			ProofAPI
//	@version		1.0
//	@description	Production-ready grammar and spell-checking API powered by LanguageTool, Redis, and Go.
//	@termsOfService	https://github.com/studio-devhub/proofapi
//
//	@contact.name	Studio DevHub
//	@contact.url	https://github.com/studio-devhub/proofapi/issues
//
//	@license.name	MIT
//	@license.url	https://github.com/studio-devhub/proofapi/blob/main/LICENSE
//
//	@host		localhost:4003
//	@BasePath	/v1
//	@schemes	http https
//
//	@securityDefinitions.apikey	ApiKeyAuth
//	@in							header
//	@name						X-API-Key
//	@description				API key required for all endpoints except /v1/health
package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	httpswagger "github.com/swaggo/http-swagger/v2"

	"languagetool-backend/internal/cache"
	"languagetool-backend/docs"
	"languagetool-backend/internal/dictionary"
	"languagetool-backend/internal/languagetool"
	"languagetool-backend/internal/middleware"
	"languagetool-backend/internal/ws"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		slog.Error("API_KEY env var is required but not set")
		os.Exit(1)
	}

	redis, err := cache.NewRedis(cache.Config{
		Host:     getEnv("REDIS_HOST", "redis"),
		Port:     getEnv("REDIS_PORT", "6379"),
		Password: getEnv("REDIS_PASSWORD", ""),
	})
	if err != nil {
		slog.Error("redis connect failed", "err", err)
		os.Exit(1)
	}
	defer redis.Close()

	ltClient := languagetool.NewClient(languagetool.Config{
		BaseURL: getEnv("LT_URL", "http://languagetool:8010"),
		Timeout: 30 * time.Second,
	})

	// Warm up the LT JVM so the first real user request isn't slow.
	go func() {
		time.Sleep(5 * time.Second)
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := ltClient.Check(ctx, languagetool.CheckRequest{
			Text: "This is a warmup request to initialize the language model.", Language: "en-US",
		}); err != nil {
			slog.Warn("lt warmup failed", "err", err)
		} else {
			slog.Info("lt warmup complete")
		}
	}()

	// ── L1 + L2 tiered cache ──────────────────────────────────
	// L1: in-process LRU (1000 entries, 5min TTL) — zero network latency
	// L2: Redis (30min TTL) — shared across restarts and instances
	l1 := cache.NewLRUCache(1000, 5*time.Minute)
	tiered := cache.NewTieredCache(l1, redis)

	// ── Dictionary (DynamoDB + Redis) ─────────────────────────
	dictSvc := buildDictionaryService(redis, logger)

	restHandler := languagetool.NewHandler(ltClient, tiered, dictSvc, logger)
	hub := ws.NewHub(logger)
	wsHandler := ws.NewHandler(hub, apiKey, ltClient, tiered, dictSvc, logger)

	r := chi.NewRouter()
	r.Use(middleware.CORS)
	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.RateLimit(200, time.Minute))

	dictHandler := dictionary.NewHTTPHandler(dictSvc, logger)

	// REST routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.APIKey(apiKey))
		r.Use(chimiddleware.Timeout(35 * time.Second))
		r.Post("/v1/check", restHandler.Check)
		r.Get("/v1/languages", restHandler.Languages)
		r.Delete("/v1/cache", restHandler.ClearCache)
	})

	// Dictionary routes (require both API key + X-Client-ID)
	r.Group(func(r chi.Router) {
		r.Use(middleware.APIKey(apiKey))
		r.Use(middleware.RequireClientID)
		r.Post("/v1/dictionary/words", dictHandler.AddWord)
		r.Get("/v1/dictionary/words", dictHandler.ListWords)
		r.Delete("/v1/dictionary/words/{word}", dictHandler.RemoveWord)
		r.Delete("/v1/dictionary", dictHandler.ClearAll)
	})

	// WebSocket route — auth via first message {type:"auth",key:"<api-key>"}
	r.Get("/v1/ws", wsHandler.ServeWS)

	// Swagger UI (no auth) — host is overridden per-request so the "Try it out"
	// button targets the correct server (localhost in dev, production domain in prod).
	r.Get("/docs", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs/index.html", http.StatusMovedPermanently)
	})
	r.Get("/docs/*", func(w http.ResponseWriter, r *http.Request) {
		docs.SwaggerInfo.Host = r.Host
		scheme := "http"
		if r.Header.Get("X-Forwarded-Proto") == "https" || r.TLS != nil {
			scheme = "https"
		}
		docs.SwaggerInfo.Schemes = []string{scheme}
		httpswagger.Handler(httpswagger.URL("/docs/doc.json")).ServeHTTP(w, r)
	})

	// Health (no auth)
	r.Get("/v1/health", healthHandler(ltClient, redis, wsHandler))

	port := getEnv("PORT", "4003")
	srv := &http.Server{
		Addr:        ":" + port,
		Handler:     r,
		ReadTimeout: 10 * time.Second,
		// WriteTimeout must be 0 for WebSocket connections: the server-level timeout
		// fires on long-lived WS connections (our ping interval is 30 s) and closes them.
		// Per-message write deadlines are set inside writePump instead.
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		slog.Info("server starting", "port", port)
		slog.Info("websocket ready", "url", "ws://localhost:"+port+"/v1/ws")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}

// healthHandler returns the /v1/health handler with injected dependencies.
//
//	@Summary		Health check
//	@Description	Returns the status of all services: API, LanguageTool, Redis, and WebSocket hub.
//	@Tags			system
//	@Produce		json
//	@Success		200	{object}	docs.HealthResponse
//	@Failure		503	{object}	docs.HealthResponse
//	@Router			/health [get]
func healthHandler(ltClient *languagetool.Client, redis *cache.Redis, wsHandler *ws.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		ltOk := ltClient.Ping(ctx)
		redisOk := redis.Ping(ctx)
		status := http.StatusOK
		if !ltOk || !redisOk {
			status = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{
			"api":          "ok",
			"languagetool": boolStatus(ltOk),
			"redis":        boolStatus(redisOk),
			"websocket":    wsHandler.Stats(),
			"cacheStats":   redis.Stats(ctx),
		})
	}
}

func buildDictionaryService(redis *cache.Redis, logger *slog.Logger) *dictionary.Service {
	region := getEnv("AWS_REGION", "us-west-2")
	tableName := getEnv("DYNAMODB_TABLE", "proofapi-dictionary")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		logger.Error("aws config failed, dictionary disabled", "err", err)
		return nil
	}

	dynamoOpts := []func(*dynamodb.Options){}
	if endpoint := os.Getenv("DYNAMODB_ENDPOINT"); endpoint != "" {
		dynamoOpts = append(dynamoOpts, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}

	dynamoClient := dynamodb.NewFromConfig(awsCfg, dynamoOpts...)
	store := dictionary.NewDynamoStore(dynamoClient, tableName)
	dictCache := dictionary.NewDictCache(redis)
	return dictionary.NewService(store, dictCache, logger)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func boolStatus(ok bool) string {
	if ok {
		return "ok"
	}
	return "unreachable"
}
