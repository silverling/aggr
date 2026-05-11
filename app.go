package main

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
	"strconv"
	"strings"
	"time"
)

type config struct {
	Addr         string
	DatabasePath string
	Environment  string
	WebDevURL    string
}

type server struct {
	cfg         config
	logger      *slog.Logger
	store       *store
	syncClient  *http.Client
	proxyClient *http.Client
	devProxy    *httputil.ReverseProxy
}

type providerPayload struct {
	Name    string `json:"name"`
	BaseURL string `json:"baseUrl"`
	APIKey  string `json:"apiKey"`
	Enabled *bool  `json:"enabled"`
}

type syncAllResponse struct {
	Results map[int64]string `json:"results"`
}

func newServer(cfg config, db *sql.DB, logger *slog.Logger) (*server, error) {
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

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

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

func (s *server) handleSyncAllProviders(w http.ResponseWriter, r *http.Request) {
	results := s.syncAllProviders(r.Context())
	writeJSON(w, http.StatusOK, syncAllResponse{
		Results: results,
	})
}

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

func (s *server) handleListOpenAIModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.store.listRouteModels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toOpenAIModels(models))
}

func (s *server) handleProxyOpenAI(w http.ResponseWriter, r *http.Request) {
	s.proxyOpenAIRequest(w, r)
}

func (s *server) handleUI(w http.ResponseWriter, r *http.Request) {
	if s.devProxy != nil {
		s.devProxy.ServeHTTP(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(embeddedIndexHTML))
}

func (s *server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("http request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(startedAt))
	})
}

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

func decodeProviderPayload(r *http.Request, allowEmptyAPIKey bool) (providerMutation, error) {
	var payload providerPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		return providerMutation{}, fmt.Errorf("decode provider payload: %w", err)
	}
	return payload.validate(allowEmptyAPIKey)
}

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

func parseProviderID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid provider id %q", raw)
	}
	return id, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{
		"error": message,
	})
}
