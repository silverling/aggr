package server

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

// Config captures the runtime settings for the HTTP server and SQLite database.
type Config struct {
	// Addr is the TCP listen address for the HTTP server, such as `:8080`.
	Addr string
	// DatabasePath is the filesystem path for the SQLite database file.
	DatabasePath string
	// AccessKey is the shared secret required to log in to the Web UI and
	// administrative APIs.
	AccessKey string
	// Environment records whether the process is running in production or dev mode.
	Environment string
}

// server owns the HTTP mux, persistence layer, and upstream clients used by
// the gateway.
type server struct {
	cfg         Config
	logger      *slog.Logger
	store       *store
	syncClient  *http.Client
	proxyClient *http.Client
}

// loggingResponseWriter wraps an HTTP response writer so the request logger can
// record the final status code without changing handler behavior.
type loggingResponseWriter struct {
	// ResponseWriter is the wrapped writer that ultimately sends the response.
	http.ResponseWriter
	// statusCode stores the final HTTP status code returned to the client.
	statusCode int
	// wroteHeader reports whether the handler explicitly or implicitly sent headers.
	wroteHeader bool
}

// providerPayload is the JSON body accepted by the provider create and update APIs.
type providerPayload struct {
	// Name is the user-facing provider label shown in the UI.
	Name string `json:"name"`
	// BaseURL is the OpenAI-compatible root endpoint for the upstream provider.
	BaseURL string `json:"baseUrl"`
	// APIKey is the bearer token used when calling the upstream provider.
	APIKey string `json:"apiKey"`
	// UserAgent is the optional upstream user-agent string used for provider requests.
	UserAgent string `json:"userAgent"`
	// Enabled toggles whether the provider can be selected for routing.
	Enabled *bool `json:"enabled"`
}

// modelDisableRulePayload is the JSON body accepted by the provider/model
// disable-rule API.
type modelDisableRulePayload struct {
	// ProviderID identifies the provider affected by the rule.
	ProviderID int64 `json:"providerId"`
	// ModelID identifies the synced model affected by the rule.
	ModelID string `json:"modelId"`
	// Disabled reports whether the rule should exist after the request completes.
	Disabled bool `json:"disabled"`
}

// modelDisableRulesPayload is the batch JSON body accepted by the provider/model
// disable-rule API when the caller wants to apply multiple route changes at once.
type modelDisableRulesPayload struct {
	// Rules contains each provider/model pair together with its desired disabled state.
	Rules []modelDisableRulePayload `json:"rules"`
}

// modelAliasPayload is the JSON body accepted by the model-alias create and
// update APIs.
type modelAliasPayload struct {
	// AliasModelID is the public model name exposed by the gateway.
	AliasModelID string `json:"aliasModelId"`
	// TargetModelID is the upstream model name that requests should use.
	TargetModelID string `json:"targetModelId"`
	// TargetProviderID optionally pins the alias to one provider.
	TargetProviderID *int64 `json:"targetProviderId"`
}

// syncAllResponse reports the per-provider result of a bulk model catalog sync.
type syncAllResponse struct {
	Results map[int64]string `json:"results"`
}

// newServer constructs the application, applies database migrations, and wires
// together the API handlers with their supporting clients.
func newServer(cfg Config, db *sql.DB, logger *slog.Logger) (*server, error) {
	if strings.TrimSpace(cfg.AccessKey) == "" {
		return nil, errors.New("access key is required")
	}

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

	return instance, nil
}

// NewHandler constructs the HTTP handler tree for the gateway so tests and
// embedders can exercise the server without calling Run.
func NewHandler(cfg Config, db *sql.DB, logger *slog.Logger) (http.Handler, error) {
	instance, err := newServer(cfg, db, logger)
	if err != nil {
		return nil, err
	}
	return instance.routes(), nil
}

// routes builds the full HTTP mux for health checks, provider admin APIs,
// OpenAI-compatible proxy endpoints, and the UI entrypoint.
func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/auth/session", s.handleAuthSession)
	mux.HandleFunc("POST /api/auth/login", s.handleAuthLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleAuthLogout)
	mux.HandleFunc("GET /api/auth/sessions", s.handleListAuthSessions)
	mux.HandleFunc("DELETE /api/auth/sessions/{id}", s.handleDeleteAuthSession)
	mux.HandleFunc("GET /api/auth/api-keys", s.handleListGatewayAPIKeys)
	mux.HandleFunc("POST /api/auth/api-keys", s.handleCreateGatewayAPIKey)
	mux.HandleFunc("DELETE /api/auth/api-keys/{id}", s.handleDeleteGatewayAPIKey)
	mux.HandleFunc("GET /api/providers", s.handleListProviders)
	mux.HandleFunc("POST /api/providers", s.handleCreateProvider)
	mux.HandleFunc("PUT /api/providers/{id}", s.handleUpdateProvider)
	mux.HandleFunc("DELETE /api/providers/{id}", s.handleDeleteProvider)
	mux.HandleFunc("POST /api/providers/{id}/sync", s.handleSyncProvider)
	mux.HandleFunc("POST /api/providers/sync", s.handleSyncAllProviders)
	mux.HandleFunc("GET /api/models", s.handleListModels)
	mux.HandleFunc("GET /api/model-aliases", s.handleListModelAliases)
	mux.HandleFunc("POST /api/model-aliases", s.handleCreateModelAlias)
	mux.HandleFunc("PUT /api/model-aliases/{id}", s.handleUpdateModelAlias)
	mux.HandleFunc("DELETE /api/model-aliases/{id}", s.handleDeleteModelAlias)
	mux.HandleFunc("PUT /api/model-disable-rules", s.handleSetModelDisableRule)
	mux.HandleFunc("GET /api/stats", s.handleGetRequestStats)
	mux.HandleFunc("GET /api/requests", s.handleListProxyRequests)
	mux.HandleFunc("DELETE /api/requests", s.handleDeleteProxyRequests)
	mux.HandleFunc("GET /v1/models", s.handleListOpenAIModels)
	mux.HandleFunc("/v1/", s.handleProxyOpenAI)
	mux.HandleFunc("/", s.handleUI)

	return s.withLogging(s.withCORS(s.withAuth(mux)))
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

// handleListModelAliases returns the configured model aliases for the Web UI.
func (s *server) handleListModelAliases(w http.ResponseWriter, r *http.Request) {
	aliases, err := s.store.listModelAliasViews(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"aliases": aliases,
	})
}

// handleCreateModelAlias inserts a new alias model and returns the persisted
// record together with its current routability snapshot.
func (s *server) handleCreateModelAlias(w http.ResponseWriter, r *http.Request) {
	mutation, err := decodeModelAliasPayload(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	alias, err := s.store.createModelAlias(r.Context(), mutation)
	if err != nil {
		s.writeModelAliasError(w, err)
		return
	}

	view, err := s.loadModelAliasView(r.Context(), alias.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"alias": view,
	})
}

// handleUpdateModelAlias updates an existing alias and returns the refreshed
// record together with its current routability snapshot.
func (s *server) handleUpdateModelAlias(w http.ResponseWriter, r *http.Request) {
	id, err := parseModelAliasID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	mutation, err := decodeModelAliasPayload(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	alias, err := s.store.updateModelAlias(r.Context(), id, mutation)
	if err != nil {
		s.writeModelAliasError(w, err)
		return
	}

	view, err := s.loadModelAliasView(r.Context(), alias.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"alias": view,
	})
}

// handleDeleteModelAlias removes one configured alias model.
func (s *server) handleDeleteModelAlias(w http.ResponseWriter, r *http.Request) {
	id, err := parseModelAliasID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.store.deleteModelAlias(r.Context(), id); err != nil {
		switch {
		case errors.Is(err, errModelAliasNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleSetModelDisableRule creates or removes one or more provider/model
// disable rules in a single request.
func (s *server) handleSetModelDisableRule(w http.ResponseWriter, r *http.Request) {
	mutations, err := decodeModelDisableRulesPayload(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.store.setProviderModelDisabledBatch(r.Context(), mutations); err != nil {
		switch {
		case errors.Is(err, errProviderNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, errProviderModelNotFound):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	response := modelDisableRulesPayload{
		Rules: make([]modelDisableRulePayload, 0, len(mutations)),
	}
	for _, mutation := range mutations {
		response.Rules = append(response.Rules, modelDisableRulePayload{
			ProviderID: mutation.ProviderID,
			ModelID:    mutation.ModelID,
			Disabled:   mutation.Disabled,
		})
	}

	writeJSON(w, http.StatusOK, response)
}

// handleGetRequestStats returns the selected summary metrics together with the
// fixed recent token-usage charts for the dashboard.
func (s *server) handleGetRequestStats(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	window, err := parseRequestStatsWindow(r.URL.Query().Get("range"), now)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	stats, err := s.store.listRequestStats(r.Context(), window, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// handleListProxyRequests returns the recent OpenAI gateway request audit log.
func (s *server) handleListProxyRequests(w http.ResponseWriter, r *http.Request) {
	limit := normalizeProxyRequestLogLimit(r.URL.Query().Get("limit"))
	requests, err := s.store.listProxyRequestLogViews(r.Context(), limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"requests": requests,
	})
}

// handleDeleteProxyRequests removes request audit rows that match the provided
// provider and date-range query filters.
func (s *server) handleDeleteProxyRequests(w http.ResponseWriter, r *http.Request) {
	providerID, err := parseOptionalProviderID(r.URL.Query().Get("providerId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	from, err := parseOptionalTimestamp(r.URL.Query().Get("from"), "from")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	to, err := parseOptionalTimestamp(r.URL.Query().Get("to"), "to")
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if from != nil && to != nil && from.After(*to) {
		writeError(w, http.StatusBadRequest, "from must be before or equal to to")
		return
	}

	deleted, err := s.store.deleteProxyRequestLogs(r.Context(), providerID, from, to)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": deleted,
	})
}

// handleListOpenAIModels returns the aggregated route table in OpenAI's models-list shape.
func (s *server) handleListOpenAIModels(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	auditContext := context.WithoutCancel(r.Context())
	logID := s.createProxyRequestAudit(auditContext, r, "", nil)

	models, err := s.store.listPublicRouteModels(r.Context())
	if err != nil {
		s.writeLoggedProxyError(w, auditContext, logID, startedAt, nil, nil, http.StatusInternalServerError, err.Error())
		return
	}

	body, err := encodeJSONPayload(toOpenAIModels(models))
	if err != nil {
		s.writeLoggedProxyError(w, auditContext, logID, startedAt, nil, nil, http.StatusInternalServerError, err.Error())
		return
	}

	responseBody, responseBodyTruncated := truncateAuditBytes(body)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	s.completeProxyRequestAudit(auditContext, logID, proxyRequestLogUpdate{
		ResponseStatus:        http.StatusOK,
		ResponseHeaders:       headersToAuditJSON(w.Header()),
		ResponseBody:          responseBody,
		ResponseBodyTruncated: responseBodyTruncated,
		DurationMS:            time.Since(startedAt).Milliseconds(),
		CompletedAt:           time.Now(),
	})

	writeJSONBytes(w, http.StatusOK, body)
}

// handleProxyOpenAI forwards OpenAI-compatible requests to the provider that serves the requested model.
func (s *server) handleProxyOpenAI(w http.ResponseWriter, r *http.Request) {
	if shouldHandleOpenAIResponsesWebSocket(r) {
		s.proxyOpenAIWebSocket(w, r)
		return
	}

	s.proxyOpenAIRequest(w, r)
}

// Run opens the SQLite database, starts the HTTP server, and blocks until the process exits.
func Run() error {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if err := loadDotEnvFile(".env"); err != nil {
		return fmt.Errorf("load .env: %w", err)
	}

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

	serverErrors := make(chan error, 1)
	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(shutdownSignals)

	logger.Info("starting aggr",
		"addr", cfg.Addr,
		"db", cfg.DatabasePath,
		"environment", cfg.Environment,
	)

	// Start the listener in a background goroutine so the main run loop can
	// react to either server failures or shutdown signals.
	go func() {
		serverErrors <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("run http server: %w", err)
		}
		return nil
	case receivedSignal := <-shutdownSignals:
		logger.Info("shutting down aggr", "signal", receivedSignal.String())
	}

	shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()

	if err := httpServer.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}

	if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("wait for http server shutdown: %w", err)
	}

	return nil
}

// loadConfig reads environment variables and fills in the defaults used to start the process.
func loadConfig() Config {
	return Config{
		Addr:         getenv("AGGR_ADDR", ":8080"),
		DatabasePath: getenv("AGGR_DB_PATH", "aggr.db"),
		AccessKey:    strings.TrimSpace(os.Getenv("AGGR_ACCESS_KEY")),
		Environment:  getenv("AGGR_ENV", "prod"),
	}
}

// getenv returns a trimmed environment variable value or the provided fallback when it is empty.
func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

// loadDotEnvFile reads a simple `.env` file from disk and populates the
// process environment without overwriting values that are already set.
func loadDotEnvFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read dotenv file: %w", err)
	}

	lines := strings.Split(string(content), "\n")
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		}

		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}

		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}

		value = strings.TrimSpace(value)
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set dotenv variable %s: %w", key, err)
		}
	}

	return nil
}

// handleUI serves the embedded dashboard HTML for the gateway.
func (s *server) handleUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(embeddedIndexHTML))
}

// withLogging wraps a handler so each request is logged with its method, path, and duration.
func (s *server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		writer := newLoggingResponseWriter(w)
		next.ServeHTTP(writer, r)
		s.logger.Info(
			"http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", writer.statusCode,
			"duration", time.Since(startedAt),
		)
	})
}

// newLoggingResponseWriter returns a response-writer wrapper initialized with
// the default `200 OK` status used by Go when handlers never override it.
func newLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}
}

// Header returns the wrapped response header map so handlers can mutate it normally.
func (writer *loggingResponseWriter) Header() http.Header {
	return writer.ResponseWriter.Header()
}

// WriteHeader records the final status code before delegating to the wrapped writer.
func (writer *loggingResponseWriter) WriteHeader(statusCode int) {
	if writer.wroteHeader {
		writer.ResponseWriter.WriteHeader(statusCode)
		return
	}

	writer.statusCode = statusCode
	writer.wroteHeader = true
	writer.ResponseWriter.WriteHeader(statusCode)
}

// Write records the implicit `200 OK` status when handlers write a body without
// calling `WriteHeader`, then delegates the bytes to the wrapped writer.
func (writer *loggingResponseWriter) Write(body []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}

	return writer.ResponseWriter.Write(body)
}

// Flush forwards HTTP streaming flushes to the wrapped writer when it supports them.
func (writer *loggingResponseWriter) Flush() {
	if flusher, ok := writer.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// Hijack forwards connection hijacking to the wrapped writer when the server is
// handling HTTP/1.x upgrades such as websocket handshakes.
func (writer *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := writer.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("wrapped response writer does not support hijacking")
	}

	return hijacker.Hijack()
}

// Push forwards HTTP/2 server-push requests to the wrapped writer when supported.
func (writer *loggingResponseWriter) Push(target string, options *http.PushOptions) error {
	pusher, ok := writer.ResponseWriter.(http.Pusher)
	if !ok {
		return http.ErrNotSupported
	}

	return pusher.Push(target, options)
}

// ReadFrom forwards optimized stream copies to the wrapped writer while still
// ensuring the logged status code reflects the implicit `200 OK` write.
func (writer *loggingResponseWriter) ReadFrom(source io.Reader) (int64, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}

	readerFrom, ok := writer.ResponseWriter.(io.ReaderFrom)
	if !ok {
		return io.Copy(writer.ResponseWriter, source)
	}

	return readerFrom.ReadFrom(source)
}

// Unwrap exposes the underlying response writer so helpers such as
// `http.NewResponseController` can reach transport-specific capabilities.
func (writer *loggingResponseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
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

// decodeModelDisableRulesPayload parses and validates either the legacy
// single-rule request body or the newer batch wrapper used by the dashboard.
func decodeModelDisableRulesPayload(r *http.Request) ([]modelDisableRuleMutation, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("read model disable rule payload: %w", err)
	}

	type batchEnvelope struct {
		// Rules stores the raw JSON payload so the decoder can distinguish between
		// an omitted field and an explicitly provided empty array.
		Rules json.RawMessage `json:"rules"`
	}

	var envelope batchEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("decode model disable rule payload: %w", err)
	}

	if len(envelope.Rules) > 0 {
		var payload modelDisableRulesPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			return nil, fmt.Errorf("decode model disable rules payload: %w", err)
		}
		if len(payload.Rules) == 0 {
			return nil, errors.New("rules must contain at least one provider/model pair")
		}

		mutations := make([]modelDisableRuleMutation, 0, len(payload.Rules))
		for index, rule := range payload.Rules {
			mutation, err := normalizeModelDisableRulePayload(rule)
			if err != nil {
				return nil, fmt.Errorf("rules[%d]: %w", index, err)
			}
			mutations = append(mutations, mutation)
		}
		return mutations, nil
	}

	var payload modelDisableRulePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode model disable rule payload: %w", err)
	}

	mutation, err := normalizeModelDisableRulePayload(payload)
	if err != nil {
		return nil, err
	}

	return []modelDisableRuleMutation{mutation}, nil
}

// normalizeModelDisableRulePayload trims and validates one provider/model
// disable-rule payload before the store applies it.
func normalizeModelDisableRulePayload(payload modelDisableRulePayload) (modelDisableRuleMutation, error) {
	if payload.ProviderID <= 0 {
		return modelDisableRuleMutation{}, errors.New("providerId must be a positive integer")
	}

	payload.ModelID = strings.TrimSpace(payload.ModelID)
	if payload.ModelID == "" {
		return modelDisableRuleMutation{}, errors.New("modelId is required")
	}

	return modelDisableRuleMutation{
		ProviderID: payload.ProviderID,
		ModelID:    payload.ModelID,
		Disabled:   payload.Disabled,
	}, nil
}

// decodeModelAliasPayload parses and validates the model-alias request body.
func decodeModelAliasPayload(r *http.Request) (modelAliasMutation, error) {
	var payload modelAliasPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return modelAliasMutation{}, fmt.Errorf("decode model alias payload: %w", err)
	}

	payload.AliasModelID = strings.TrimSpace(payload.AliasModelID)
	payload.TargetModelID = strings.TrimSpace(payload.TargetModelID)
	if payload.AliasModelID == "" {
		return modelAliasMutation{}, errors.New("aliasModelId is required")
	}
	if payload.TargetModelID == "" {
		return modelAliasMutation{}, errors.New("targetModelId is required")
	}
	if payload.TargetProviderID != nil && *payload.TargetProviderID <= 0 {
		return modelAliasMutation{}, errors.New("targetProviderId must be a positive integer")
	}

	return modelAliasMutation{
		AliasModelID:     payload.AliasModelID,
		TargetModelID:    payload.TargetModelID,
		TargetProviderID: payload.TargetProviderID,
	}, nil
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
		Name:      name,
		BaseURL:   baseURL,
		APIKey:    apiKey,
		UserAgent: strings.TrimSpace(payload.UserAgent),
		Enabled:   enabled,
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

// parseModelAliasID converts a path parameter into a positive alias identifier.
func parseModelAliasID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid model alias id %q", raw)
	}
	return id, nil
}

// parseOptionalProviderID converts an optional query parameter into a provider
// identifier while allowing the caller to omit the filter entirely.
func parseOptionalProviderID(raw string) (*int64, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	id, err := parseProviderID(raw)
	if err != nil {
		return nil, err
	}

	return &id, nil
}

// parseOptionalTimestamp converts an optional RFC3339 query parameter into a
// UTC timestamp pointer for request-log filtering.
func parseOptionalTimestamp(raw string, fieldName string) (*time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}

	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid %s timestamp %q: must be RFC3339", fieldName, raw)
	}

	utc := parsed.UTC()
	return &utc, nil
}

// parseRequestStatsWindow converts the dashboard range selector into the
// selected lower-bound timestamp used by the stats API.
func parseRequestStatsWindow(raw string, now time.Time) (requestStatsWindow, error) {
	switch strings.TrimSpace(raw) {
	case "", "24h":
		from := now.Add(-24 * time.Hour)
		return requestStatsWindow{Key: "24h", Label: "In 24 hours", From: &from}, nil
	case "1h":
		from := now.Add(-1 * time.Hour)
		return requestStatsWindow{Key: "1h", Label: "In an hour", From: &from}, nil
	case "7d":
		from := now.AddDate(0, 0, -7)
		return requestStatsWindow{Key: "7d", Label: "In a week", From: &from}, nil
	case "30d":
		from := now.AddDate(0, -1, 0)
		return requestStatsWindow{Key: "30d", Label: "In a month", From: &from}, nil
	case "all":
		return requestStatsWindow{Key: "all", Label: "All"}, nil
	default:
		return requestStatsWindow{}, fmt.Errorf("invalid stats range %q", raw)
	}
}

// writeJSON writes a JSON response with the provided status code.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSONBytes(w, status, body)
}

// writeJSONBytes writes a JSON response body that has already been encoded.
func writeJSONBytes(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// writeError writes a JSON error envelope with the provided status code.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}

// loadModelAliasView returns one alias record together with its current
// routability snapshot for the dashboard response bodies.
func (s *server) loadModelAliasView(ctx context.Context, id int64) (modelAliasView, error) {
	alias, err := s.store.getModelAlias(ctx, id)
	if err != nil {
		return modelAliasView{}, err
	}

	providers, err := s.store.listProvidersForAlias(ctx, alias)
	if err != nil {
		return modelAliasView{}, err
	}

	return alias.toView(providers), nil
}

// writeModelAliasError maps model-alias store errors into the most appropriate
// HTTP status code for the admin API.
func (s *server) writeModelAliasError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errModelAliasNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errModelAliasConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, errModelAliasTarget):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, errProviderNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, errProviderModelNotFound):
		writeError(w, http.StatusBadRequest, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
}
