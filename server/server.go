package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Config captures the runtime settings for the HTTP server, SQLite database,
// and development UI proxy.
type Config struct {
	// Addr is the TCP listen address for the HTTP server, such as `:8080`.
	Addr string
	// DatabasePath is the filesystem path for the SQLite database file.
	DatabasePath string
	// Environment records whether the process is running in production or dev mode.
	Environment string
	// WebDevURL points at the Vite dev server when the UI should be proxied live.
	WebDevURL string
}

// server owns the HTTP mux, persistence layer, upstream clients, and optional
// dev proxy used to serve the UI in development.
type server struct {
	cfg         Config
	logger      *slog.Logger
	store       *store
	syncClient  *http.Client
	proxyClient *http.Client
	devProxy    *httputil.ReverseProxy
}

// providerPayload is the JSON body accepted by the provider create and update APIs.
type providerPayload struct {
	// Name is the user-facing provider label shown in the UI.
	Name string `json:"name"`
	// BaseURL is the OpenAI-compatible root endpoint for the upstream provider.
	BaseURL string `json:"baseUrl"`
	// APIKey is the bearer token used when calling the upstream provider.
	APIKey string `json:"apiKey"`
	// Enabled toggles whether the provider can be selected for routing.
	Enabled *bool `json:"enabled"`
}

// syncAllResponse reports the per-provider result of a bulk model catalog sync.
type syncAllResponse struct {
	Results map[int64]string `json:"results"`
}

// newServer constructs the application, applies database migrations, and wires
// together the API handlers with their supporting clients.
func newServer(cfg Config, db *sql.DB, logger *slog.Logger) (*server, error) {
	st := newStore(db)
	if err := st.migrate(context.Background()); err != nil {
		return nil, err
	}

	instance := &server{
		cfg:    cfg,
		logger: logger,
		store:  st,
		syncClient: &http.Client{
			Timeout: 20 * time.Second,
		},
		proxyClient: &http.Client{},
	}

	if cfg.WebDevURL != "" {
		parsed, err := url.Parse(cfg.WebDevURL)
		if err != nil {
			return nil, fmt.Errorf("parse AGGR_WEB_DEV_URL: %w", err)
		}
		instance.devProxy = httputil.NewSingleHostReverseProxy(parsed)
		instance.devProxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, proxyErr error) {
			logger.Error("proxy vite dev server", "error", proxyErr)
			writeError(w, http.StatusBadGateway, "vite dev server is unavailable")
		}
	}

	return instance, nil
}

// routes builds the full HTTP mux for health checks, provider admin APIs,
// OpenAI-compatible proxy endpoints, and the UI entrypoint.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/providers", s.handleListProviders)
	mux.HandleFunc("POST /api/providers", s.handleCreateProvider)
	mux.HandleFunc("PUT /api/providers/{id}", s.handleUpdateProvider)
	mux.HandleFunc("DELETE /api/providers/{id}", s.handleDeleteProvider)
	mux.HandleFunc("POST /api/providers/{id}/sync", s.handleSyncProvider)
	mux.HandleFunc("POST /api/providers/sync", s.handleSyncAllProviders)
	mux.HandleFunc("GET /api/models", s.handleListModels)
	mux.HandleFunc("GET /v1/models", s.handleListOpenAIModels)
	mux.HandleFunc("/v1/", s.handleProxyOpenAI)
	mux.HandleFunc("/", s.handleUI)

	return s.withLogging(s.withCORS(mux))
}

// handleHealth returns a small JSON payload used for liveness and readiness probes.
func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

// handleListProviders returns the configured providers with their synced model lists.
func (s *server) handleListProviders(w http.ResponseWriter, r *http.Request) {
	providers, err := s.store.listProviderViews(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"providers": providers,
	})
}

// handleCreateProvider inserts a new provider, syncs its model catalog, and returns the saved record.
func (s *server) handleCreateProvider(w http.ResponseWriter, r *http.Request) {
	mutation, err := decodeProviderPayload(r, false)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	provider, err := s.store.createProvider(r.Context(), mutation)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.syncProviderCatalog(r.Context(), provider); err != nil {
		s.logger.Warn("initial provider sync failed", "provider_id", provider.ID, "error", err)
	}

	updated, err := s.store.getProviderWithModels(r.Context(), provider.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"provider": updated.toView(),
	})
}

// handleUpdateProvider updates a provider, refreshes its model catalog, and returns the saved record.
func (s *server) handleUpdateProvider(w http.ResponseWriter, r *http.Request) {
	id, err := parseProviderID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	mutation, keepAPIKey, err := decodeProviderPayloadForUpdate(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	provider, err := s.store.updateProvider(r.Context(), id, mutation, keepAPIKey)
	if err != nil {
		if errors.Is(err, errProviderNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.syncProviderCatalog(r.Context(), provider); err != nil {
		s.logger.Warn("provider sync failed after update", "provider_id", provider.ID, "error", err)
	}

	updated, err := s.store.getProviderWithModels(r.Context(), provider.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"provider": updated.toView(),
	})
}

// handleDeleteProvider removes a provider and cascades its model mappings.
func (s *server) handleDeleteProvider(w http.ResponseWriter, r *http.Request) {
	id, err := parseProviderID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.store.deleteProvider(r.Context(), id); err != nil {
		if errors.Is(err, errProviderNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleSyncProvider refreshes a single provider's model catalog from its upstream API.
func (s *server) handleSyncProvider(w http.ResponseWriter, r *http.Request) {
	id, err := parseProviderID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	provider, err := s.store.getProviderWithModels(r.Context(), id)
	if err != nil {
		if errors.Is(err, errProviderNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.syncProviderCatalog(r.Context(), provider); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	updated, err := s.store.getProviderWithModels(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"provider": updated.toView(),
	})
}

// handleSyncAllProviders refreshes every configured provider and returns the outcome map.
func (s *server) handleSyncAllProviders(w http.ResponseWriter, r *http.Request) {
	results := s.syncAllProviders(r.Context())
	writeJSON(w, http.StatusOK, syncAllResponse{
		Results: results,
	})
}

// handleListModels returns the aggregated route table used by the UI.
func (s *server) handleListModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.store.listRouteModels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"models": models,
	})
}

// handleListOpenAIModels returns the aggregated route table in OpenAI's models-list shape.
func (s *server) handleListOpenAIModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.store.listRouteModels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toOpenAIModels(models))
}

// handleProxyOpenAI forwards OpenAI-compatible requests to the provider that serves the requested model.
func (s *server) handleProxyOpenAI(w http.ResponseWriter, r *http.Request) {
	s.proxyOpenAIRequest(w, r)
}

// Run opens the SQLite database, starts the HTTP server, and blocks until the process exits.
func Run() error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	cfg := loadConfig()

	db, err := sql.Open("sqlite", cfg.DatabasePath)
	if err != nil {
		return fmt.Errorf("open sqlite database: %w", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	db.SetConnMaxIdleTime(5 * time.Minute)

	app, err := newServer(cfg, db, logger)
	if err != nil {
		return fmt.Errorf("initialize server: %w", err)
	}

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           app.routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	logger.Info("starting aggr",
		"addr", cfg.Addr,
		"db", cfg.DatabasePath,
		"environment", cfg.Environment,
		"web_dev_url", cfg.WebDevURL,
	)

	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("run http server: %w", err)
	}

	return nil
}

// loadConfig reads environment variables and fills in the defaults used to start the process.
func loadConfig() Config {
	environment := getenv("AGGR_ENV", "prod")
	webDevURL := strings.TrimSpace(os.Getenv("AGGR_WEB_DEV_URL"))
	if environment == "dev" && webDevURL == "" {
		webDevURL = "http://127.0.0.1:5173"
	}

	return Config{
		Addr:         getenv("AGGR_ADDR", ":8080"),
		DatabasePath: getenv("AGGR_DB_PATH", "aggr.db"),
		Environment:  environment,
		WebDevURL:    webDevURL,
	}
}

// getenv returns a trimmed environment variable value or the provided fallback when it is empty.
func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// handleUI serves the dev proxy during local development and the embedded HTML in production.
func (s *server) handleUI(w http.ResponseWriter, r *http.Request) {
	if s.devProxy != nil {
		s.devProxy.ServeHTTP(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(embeddedIndexHTML))
}

// withLogging wraps a handler so each request is logged with its method, path, and duration.
func (s *server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(startedAt))
	})
}

// withCORS adds permissive CORS headers so the UI and external clients can call the API.
func (s *server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// decodeProviderPayload parses the provider form body and validates the required fields.
func decodeProviderPayload(r *http.Request, allowEmptyAPIKey bool) (providerMutation, error) {
	var payload providerPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return providerMutation{}, fmt.Errorf("decode provider payload: %w", err)
	}
	return payload.validate(allowEmptyAPIKey)
}

// decodeProviderPayloadForUpdate parses update payloads and reports whether the API key should be preserved.
func decodeProviderPayloadForUpdate(r *http.Request) (providerMutation, bool, error) {
	var payload providerPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return providerMutation{}, false, fmt.Errorf("decode provider payload: %w", err)
	}

	mutation, err := payload.validate(true)
	if err != nil {
		return providerMutation{}, false, err
	}

	return mutation, strings.TrimSpace(payload.APIKey) == "", nil
}

// validate normalizes the provider payload and converts it into a store mutation.
func (payload providerPayload) validate(allowEmptyAPIKey bool) (providerMutation, error) {
	name := strings.TrimSpace(payload.Name)
	if name == "" {
		return providerMutation{}, errors.New("name is required")
	}

	baseURL, err := normalizeBaseURL(payload.BaseURL)
	if err != nil {
		return providerMutation{}, err
	}

	apiKey := strings.TrimSpace(payload.APIKey)
	if !allowEmptyAPIKey && apiKey == "" {
		return providerMutation{}, errors.New("api key is required")
	}

	enabled := true
	if payload.Enabled != nil {
		enabled = *payload.Enabled
	}

	return providerMutation{
		Name:    name,
		BaseURL: baseURL,
		APIKey:  apiKey,
		Enabled: enabled,
	}, nil
}

// parseProviderID converts a path parameter into a positive provider identifier.
func parseProviderID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid provider id %q", raw)
	}
	return id, nil
}

// writeJSON writes a JSON response with the provided status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// writeError writes a JSON error envelope with the provided status code.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}
