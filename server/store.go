package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

var errProviderNotFound = errors.New("provider not found")

// providerRecord is the internal database representation of a configured provider.
type providerRecord struct {
	// ID is the database primary key for the provider.
	ID int64
	// Name is the user-visible provider label.
	Name string
	// BaseURL is the upstream OpenAI-compatible API root.
	BaseURL string
	// APIKey is the bearer token stored for the provider.
	APIKey string
	// UserAgent is the optional upstream user-agent string configured for the provider.
	UserAgent string
	// Enabled controls whether the provider may be selected for routing.
	Enabled bool
	// Models contains the synced model IDs currently associated with the provider.
	Models []string
	// LastError stores the most recent catalog sync error message, if any.
	LastError string
	// LastSyncedAt records when the provider catalog was last refreshed.
	LastSyncedAt *time.Time
	// CreatedAt records when the provider row was inserted.
	CreatedAt time.Time
	// UpdatedAt records when the provider row was last modified.
	UpdatedAt time.Time
}

// providerView is the redacted provider payload returned to the UI.
type providerView struct {
	// ID is the provider identifier.
	ID int64 `json:"id"`
	// Name is the display label shown in the UI.
	Name string `json:"name"`
	// BaseURL is the upstream endpoint configured for the provider.
	BaseURL string `json:"baseUrl"`
	// UserAgent is the upstream user-agent string configured for the provider.
	UserAgent string `json:"userAgent,omitempty"`
	// Enabled reports whether the provider can participate in routing.
	Enabled bool `json:"enabled"`
	// Models lists the provider's synced model IDs.
	Models []string `json:"models"`
	// LastError exposes the most recent sync error, if one exists.
	LastError string `json:"lastError,omitempty"`
	// LastSyncedAt is the UTC timestamp of the last successful sync.
	LastSyncedAt string `json:"lastSyncedAt,omitempty"`
	// APIKeyConfigured indicates whether a key has been stored for the provider.
	APIKeyConfigured bool `json:"apiKeyConfigured"`
	// APIKeyPreview shows a masked preview of the stored API key.
	APIKeyPreview string `json:"apiKeyPreview,omitempty"`
}

// routeProviderView is the public provider summary attached to an aggregated model.
type routeProviderView struct {
	// ID is the provider identifier.
	ID int64 `json:"id"`
	// Name is the provider name used in the UI.
	Name string `json:"name"`
}

// routeModelView is the internal aggregation structure for one routable model.
type routeModelView struct {
	// ID is the model identifier.
	ID string `json:"id"`
	// Providers lists the enabled providers that currently serve the model.
	Providers []routeProviderView `json:"providers"`
}

// proxyRequestReceivedRequestView is the received-request section exposed by
// the audit API and rendered in the UI.
type proxyRequestReceivedRequestView struct {
	// Method is the incoming HTTP verb.
	Method string `json:"method"`
	// Path is the incoming request path.
	Path string `json:"path"`
	// RawQuery is the incoming request query string.
	RawQuery string `json:"rawQuery,omitempty"`
	// Headers stores the sanitized inbound headers as JSON.
	Headers string `json:"headers"`
	// Body stores a capped copy of the inbound request body.
	Body string `json:"body,omitempty"`
	// BodyTruncated reports whether the stored request body preview was shortened.
	BodyTruncated bool `json:"bodyTruncated"`
}

// proxyRequestSentRequestView is the sent-request section exposed by the audit
// API and rendered in the UI.
type proxyRequestSentRequestView struct {
	// Method is the outbound HTTP verb sent upstream.
	Method string `json:"method"`
	// URL is the exact upstream URL that the gateway called.
	URL string `json:"url"`
	// Headers stores the sanitized outbound headers as JSON.
	Headers string `json:"headers"`
	// Body stores a capped copy of the outbound request body.
	Body string `json:"body,omitempty"`
	// BodyTruncated reports whether the stored request body preview was shortened.
	BodyTruncated bool `json:"bodyTruncated"`
}

// proxyRequestReceivedResponseView is the received-response section exposed by
// the audit API and rendered in the UI.
type proxyRequestReceivedResponseView struct {
	// Status is the HTTP status returned by the upstream provider or gateway.
	Status int `json:"status,omitempty"`
	// Headers stores the final response headers as JSON.
	Headers string `json:"headers,omitempty"`
	// Body stores a capped copy of the final response body.
	Body string `json:"body,omitempty"`
	// BodyTruncated reports whether the stored response body preview was shortened.
	BodyTruncated bool `json:"bodyTruncated"`
	// Error stores the final error message, if the request failed.
	Error string `json:"error,omitempty"`
}

// proxyRequestLogRecord is the internal database representation of one audited
// gateway request and its recorded upstream response.
type proxyRequestLogRecord struct {
	// ID is the audit row primary key.
	ID int64
	// ProviderID is the provider selected for the request, if any.
	ProviderID sql.NullInt64
	// ProviderName is the provider label captured at request time.
	ProviderName string
	// ModelID is the OpenAI model identifier that routed the request.
	ModelID string
	// Method is the inbound HTTP method.
	Method string
	// Path is the inbound request path.
	Path string
	// RawQuery is the raw query string from the inbound request.
	RawQuery string
	// RequestHeaders stores the sanitized inbound headers as JSON.
	RequestHeaders string
	// RequestBody stores a capped copy of the inbound body.
	RequestBody string
	// RequestBodyTruncated reports whether the stored request body preview was shortened.
	RequestBodyTruncated bool
	// SentMethod is the outbound HTTP verb sent upstream.
	SentMethod string
	// SentURL is the exact upstream URL that the gateway called.
	SentURL string
	// SentHeaders stores the sanitized outbound headers as JSON.
	SentHeaders string
	// SentBody stores a capped copy of the outbound request body.
	SentBody string
	// SentBodyTruncated reports whether the stored request body preview was shortened.
	SentBodyTruncated bool
	// ResponseStatus is the final status code returned to the caller.
	ResponseStatus sql.NullInt64
	// ResponseHeaders stores the final response headers as JSON.
	ResponseHeaders string
	// ResponseBody stores a capped copy of the final response body.
	ResponseBody string
	// ResponseBodyTruncated reports whether the stored response body preview was shortened.
	ResponseBodyTruncated bool
	// ErrorText stores the final error message, if the request failed.
	ErrorText string
	// DurationMS stores the elapsed time in milliseconds.
	DurationMS sql.NullInt64
	// RequestedAt records when the request log row was created.
	RequestedAt time.Time
	// CompletedAt records when the request log row was finalized.
	CompletedAt sql.NullTime
}

// proxyRequestLogView is the JSON payload returned to the dashboard inspector.
type proxyRequestLogView struct {
	// ID is the audit row primary key.
	ID int64 `json:"id"`
	// ProviderID is the provider selected for the request, if any.
	ProviderID int64 `json:"providerId,omitempty"`
	// ProviderName is the provider label captured at request time.
	ProviderName string `json:"providerName,omitempty"`
	// ModelID is the OpenAI model identifier that routed the request.
	ModelID string `json:"modelId,omitempty"`
	// ReceivedRequest contains the inbound request details from the caller.
	ReceivedRequest proxyRequestReceivedRequestView `json:"receivedRequest"`
	// SentRequest contains the upstream request details when the gateway proxied the call.
	SentRequest *proxyRequestSentRequestView `json:"sentRequest,omitempty"`
	// ReceivedResponse contains the upstream response details or the final gateway error.
	ReceivedResponse proxyRequestReceivedResponseView `json:"receivedResponse"`
	// DurationMS stores the elapsed time in milliseconds.
	DurationMS int64 `json:"durationMs,omitempty"`
	// RequestedAt records when the request log row was created.
	RequestedAt string `json:"requestedAt"`
	// CompletedAt records when the request log row was finalized.
	CompletedAt string `json:"completedAt,omitempty"`
}

// proxyRequestLogCreate captures the request-side fields inserted when a log
// row is first created.
type proxyRequestLogCreate struct {
	// Method is the inbound HTTP method.
	Method string
	// Path is the inbound request path.
	Path string
	// RawQuery is the raw query string from the inbound request.
	RawQuery string
	// ModelID is the model identifier that the request is targeting.
	ModelID string
	// RequestHeaders stores the sanitized inbound headers as JSON.
	RequestHeaders string
	// RequestBody stores a capped copy of the inbound body.
	RequestBody string
	// RequestBodyTruncated reports whether the stored request body preview was shortened.
	RequestBodyTruncated bool
	// RequestedAt records when the log row was created.
	RequestedAt time.Time
}

// proxyRequestLogUpdate captures the sent-request and response-side fields
// written when a log row is finalized.
type proxyRequestLogUpdate struct {
	// SentMethod is the outbound HTTP verb sent upstream.
	SentMethod string
	// SentURL is the exact upstream URL that the gateway called.
	SentURL string
	// SentHeaders stores the sanitized outbound headers as JSON.
	SentHeaders string
	// SentBody stores a capped copy of the outbound request body.
	SentBody string
	// SentBodyTruncated reports whether the stored request body preview was shortened.
	SentBodyTruncated bool
	// ProviderID is the selected provider, if any.
	ProviderID *int64
	// ProviderName is the provider label captured at request time.
	ProviderName string
	// ResponseStatus is the final status code returned to the caller.
	ResponseStatus int
	// ResponseHeaders stores the final response headers as JSON.
	ResponseHeaders string
	// ResponseBody stores a capped copy of the final response body.
	ResponseBody string
	// ResponseBodyTruncated reports whether the stored response body preview was shortened.
	ResponseBodyTruncated bool
	// ErrorText stores the final error message, if the request failed.
	ErrorText string
	// DurationMS stores the elapsed time in milliseconds.
	DurationMS int64
	// CompletedAt records when the request finished.
	CompletedAt time.Time
}

// providerMutation captures the normalized fields used when creating or updating a provider.
type providerMutation struct {
	// Name is the normalized provider label.
	Name string
	// BaseURL is the normalized upstream base URL.
	BaseURL string
	// APIKey is the trimmed upstream API key.
	APIKey string
	// UserAgent is the trimmed upstream user-agent string.
	UserAgent string
	// Enabled indicates whether the provider should be routable.
	Enabled bool
}

// store wraps the SQLite connection and exposes all persistence operations.
type store struct {
	db *sql.DB
}

// newStore creates a store backed by the provided database handle.
func newStore(db *sql.DB) *store {
	return &store{db: db}
}

// migrate creates the tables and indexes required by the application.
func (s *store) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS providers (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			base_url TEXT NOT NULL,
			api_key TEXT NOT NULL,
			user_agent TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			last_error TEXT,
			last_synced_at TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS provider_models (
			provider_id INTEGER NOT NULL,
			model_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (provider_id, model_id),
			FOREIGN KEY (provider_id) REFERENCES providers(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_provider_models_model_id ON provider_models (model_id);`,
		`CREATE TABLE IF NOT EXISTS proxy_request_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id INTEGER,
			provider_name TEXT,
			model_id TEXT,
			method TEXT NOT NULL,
			path TEXT NOT NULL,
			raw_query TEXT NOT NULL,
			request_headers TEXT NOT NULL,
			request_body TEXT NOT NULL,
			request_body_truncated INTEGER NOT NULL DEFAULT 0,
			sent_request_method TEXT NOT NULL DEFAULT '',
			sent_request_url TEXT NOT NULL DEFAULT '',
			sent_request_headers TEXT NOT NULL DEFAULT '',
			sent_request_body TEXT NOT NULL DEFAULT '',
			sent_request_body_truncated INTEGER NOT NULL DEFAULT 0,
			response_status INTEGER,
			response_headers TEXT,
			response_body TEXT,
			response_body_truncated INTEGER NOT NULL DEFAULT 0,
			error_text TEXT,
			duration_ms INTEGER,
			requested_at TEXT NOT NULL,
			completed_at TEXT,
			FOREIGN KEY (provider_id) REFERENCES providers(id) ON DELETE SET NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_proxy_request_logs_requested_at ON proxy_request_logs (requested_at DESC, id DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_proxy_request_logs_provider_id ON proxy_request_logs (provider_id, requested_at DESC, id DESC);`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply migration: %w", err)
		}
	}

	if err := s.ensureProviderUserAgentColumn(ctx); err != nil {
		return err
	}
	if err := s.ensureProxyRequestSentRequestColumns(ctx); err != nil {
		return err
	}

	return nil
}

// ensureProviderUserAgentColumn adds the legacy provider user-agent column when
// migrating a database created before the field existed.
func (s *store) ensureProviderUserAgentColumn(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(providers)`)
	if err != nil {
		return fmt.Errorf("inspect provider schema: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("scan provider schema: %w", err)
		}
		if name == "user_agent" {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate provider schema: %w", err)
	}

	if _, err := s.db.ExecContext(ctx, `ALTER TABLE providers ADD COLUMN user_agent TEXT NOT NULL DEFAULT ''`); err != nil {
		return fmt.Errorf("add provider user_agent column: %w", err)
	}

	return nil
}

// ensureProxyRequestSentRequestColumns adds the sent-request audit columns when
// migrating a database created before the field existed.
func (s *store) ensureProxyRequestSentRequestColumns(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(proxy_request_logs)`)
	if err != nil {
		return fmt.Errorf("inspect proxy request log schema: %w", err)
	}
	defer rows.Close()

	hasSentMethod := false
	hasSentURL := false
	hasSentHeaders := false
	hasSentBody := false
	hasSentBodyTruncated := false

	for rows.Next() {
		var (
			cid        int
			name       string
			columnType string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return fmt.Errorf("scan proxy request log schema: %w", err)
		}
		switch name {
		case "sent_request_method":
			hasSentMethod = true
		case "sent_request_url":
			hasSentURL = true
		case "sent_request_headers":
			hasSentHeaders = true
		case "sent_request_body":
			hasSentBody = true
		case "sent_request_body_truncated":
			hasSentBodyTruncated = true
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate proxy request log schema: %w", err)
	}

	if !hasSentMethod {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE proxy_request_logs ADD COLUMN sent_request_method TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add proxy request sent_method column: %w", err)
		}
	}
	if !hasSentURL {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE proxy_request_logs ADD COLUMN sent_request_url TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add proxy request sent_url column: %w", err)
		}
	}
	if !hasSentHeaders {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE proxy_request_logs ADD COLUMN sent_request_headers TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add proxy request sent_headers column: %w", err)
		}
	}
	if !hasSentBody {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE proxy_request_logs ADD COLUMN sent_request_body TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("add proxy request sent_body column: %w", err)
		}
	}
	if !hasSentBodyTruncated {
		if _, err := s.db.ExecContext(ctx, `ALTER TABLE proxy_request_logs ADD COLUMN sent_request_body_truncated INTEGER NOT NULL DEFAULT 0`); err != nil {
			return fmt.Errorf("add proxy request sent_body_truncated column: %w", err)
		}
	}

	return nil
}

// listProviders returns all providers ordered by enabled status and name.
func (s *store) listProviders(ctx context.Context) ([]providerRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, base_url, api_key, user_agent, enabled, last_error, last_synced_at, created_at, updated_at
		FROM providers
		ORDER BY enabled DESC, name ASC, id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query providers: %w", err)
	}
	defer rows.Close()

	providers := make([]providerRecord, 0)
	for rows.Next() {
		record, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		providers = append(providers, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate providers: %w", err)
	}

	if err := s.attachModels(ctx, providers); err != nil {
		return nil, err
	}

	return providers, nil
}

// listProviderViews returns redacted provider data for the UI.
func (s *store) listProviderViews(ctx context.Context) ([]providerView, error) {
	providers, err := s.listProviders(ctx)
	if err != nil {
		return nil, err
	}

	views := make([]providerView, 0, len(providers))
	for _, provider := range providers {
		views = append(views, provider.toView())
	}

	return views, nil
}

// getProvider retrieves a provider together with its synced models.
func (s *store) getProvider(ctx context.Context, id int64) (providerRecord, error) {
	return s.getProviderWithModels(ctx, id)
}

// getProviderWithModels loads one provider and its model list from the database.
func (s *store) getProviderWithModels(ctx context.Context, id int64) (providerRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, base_url, api_key, user_agent, enabled, last_error, last_synced_at, created_at, updated_at
		FROM providers
		WHERE id = ?
	`, id)

	record, err := scanProvider(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return providerRecord{}, errProviderNotFound
		}
		return providerRecord{}, err
	}

	models, err := s.listModelsForProvider(ctx, id)
	if err != nil {
		return providerRecord{}, err
	}
	record.Models = models
	return record, nil
}

// createProvider inserts a provider and returns the persisted row.
func (s *store) createProvider(ctx context.Context, mutation providerMutation) (providerRecord, error) {
	now := nowRFC3339()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO providers (name, base_url, api_key, user_agent, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, mutation.Name, mutation.BaseURL, mutation.APIKey, mutation.UserAgent, boolToInt(mutation.Enabled), now, now)
	if err != nil {
		return providerRecord{}, fmt.Errorf("insert provider: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return providerRecord{}, fmt.Errorf("read provider id: %w", err)
	}

	return s.getProviderWithModels(ctx, id)
}

// updateProvider updates a provider while optionally preserving the stored API key.
func (s *store) updateProvider(ctx context.Context, id int64, mutation providerMutation, keepAPIKey bool) (providerRecord, error) {
	current, err := s.getProviderWithModels(ctx, id)
	if err != nil {
		return providerRecord{}, err
	}

	apiKey := mutation.APIKey
	if keepAPIKey {
		apiKey = current.APIKey
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE providers
		SET name = ?, base_url = ?, api_key = ?, user_agent = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, mutation.Name, mutation.BaseURL, apiKey, mutation.UserAgent, boolToInt(mutation.Enabled), nowRFC3339(), id)
	if err != nil {
		return providerRecord{}, fmt.Errorf("update provider: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return providerRecord{}, fmt.Errorf("read updated rows: %w", err)
	}
	if affected == 0 {
		return providerRecord{}, errProviderNotFound
	}

	return s.getProviderWithModels(ctx, id)
}

// deleteProvider removes a provider and its model mappings.
func (s *store) deleteProvider(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM providers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete provider: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted rows: %w", err)
	}
	if affected == 0 {
		return errProviderNotFound
	}

	return nil
}

// createProxyRequestLog inserts a new request audit record and returns its row ID.
func (s *store) createProxyRequestLog(ctx context.Context, entry proxyRequestLogCreate) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO proxy_request_logs (
			provider_id, provider_name, model_id, method, path, raw_query,
			request_headers, request_body, request_body_truncated, requested_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, nil, "", entry.ModelID, entry.Method, entry.Path, entry.RawQuery, entry.RequestHeaders, entry.RequestBody, boolToInt(entry.RequestBodyTruncated), entry.RequestedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, fmt.Errorf("insert proxy request log: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read proxy request log id: %w", err)
	}

	return id, nil
}

// completeProxyRequestLog finalizes a request audit record with the response metadata.
func (s *store) completeProxyRequestLog(ctx context.Context, id int64, update proxyRequestLogUpdate) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE proxy_request_logs
		SET provider_id = ?, provider_name = ?, sent_request_method = ?, sent_request_url = ?, sent_request_headers = ?, sent_request_body = ?, sent_request_body_truncated = ?,
			response_status = ?, response_headers = ?, response_body = ?,
			response_body_truncated = ?, error_text = ?, duration_ms = ?, completed_at = ?
		WHERE id = ?
	`, nullableInt64(update.ProviderID), update.ProviderName, update.SentMethod, update.SentURL, update.SentHeaders, update.SentBody, boolToInt(update.SentBodyTruncated), update.ResponseStatus, update.ResponseHeaders, update.ResponseBody, boolToInt(update.ResponseBodyTruncated), update.ErrorText, update.DurationMS, update.CompletedAt.UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("update proxy request log: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read proxy request log rows: %w", err)
	}
	if affected == 0 {
		return fmt.Errorf("proxy request log %d not found", id)
	}

	return nil
}

// deleteProxyRequestLogs removes request audit rows that match the provided filters.
func (s *store) deleteProxyRequestLogs(ctx context.Context, providerID *int64, from, to *time.Time) (int64, error) {
	clauses := []string{"1 = 1"}
	args := make([]any, 0, 3)

	if providerID != nil {
		clauses = append(clauses, "provider_id = ?")
		args = append(args, *providerID)
	}
	if from != nil {
		clauses = append(clauses, "requested_at >= ?")
		args = append(args, from.UTC().Format(time.RFC3339))
	}
	if to != nil {
		clauses = append(clauses, "requested_at <= ?")
		args = append(args, to.UTC().Format(time.RFC3339))
	}

	query := fmt.Sprintf("DELETE FROM proxy_request_logs WHERE %s", strings.Join(clauses, " AND "))
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("delete proxy request logs: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read deleted proxy request log rows: %w", err)
	}

	return affected, nil
}

// listProxyRequestLogs returns recent request audit rows ordered from newest to oldest.
func (s *store) listProxyRequestLogs(ctx context.Context, limit int) ([]proxyRequestLogRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT
			id, provider_id, provider_name, model_id, method, path, raw_query,
			request_headers, request_body, request_body_truncated,
			sent_request_method, sent_request_url, sent_request_headers, sent_request_body, sent_request_body_truncated,
			response_status, response_headers, response_body, response_body_truncated,
			error_text, duration_ms, requested_at, completed_at
		FROM proxy_request_logs
		ORDER BY requested_at DESC, id DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("query proxy request logs: %w", err)
	}
	defer rows.Close()

	logs := make([]proxyRequestLogRecord, 0, limit)
	for rows.Next() {
		record, err := scanProxyRequestLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate proxy request logs: %w", err)
	}

	return logs, nil
}

// listProxyRequestLogViews returns recent request audit rows in a JSON-friendly shape.
func (s *store) listProxyRequestLogViews(ctx context.Context, limit int) ([]proxyRequestLogView, error) {
	logs, err := s.listProxyRequestLogs(ctx, limit)
	if err != nil {
		return nil, err
	}

	views := make([]proxyRequestLogView, 0, len(logs))
	for _, log := range logs {
		views = append(views, log.toView())
	}

	return views, nil
}

// syncProviderModels replaces the provider's model mapping set with the provided IDs.
func (s *store) syncProviderModels(ctx context.Context, id int64, modelIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin model sync transaction: %w", err)
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	if _, err = tx.ExecContext(ctx, `DELETE FROM provider_models WHERE provider_id = ?`, id); err != nil {
		return fmt.Errorf("clear provider models: %w", err)
	}

	insertedAt := nowRFC3339()
	for _, modelID := range uniqueStrings(modelIDs) {
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO provider_models (provider_id, model_id, created_at)
			VALUES (?, ?, ?)
		`, id, modelID, insertedAt); err != nil {
			return fmt.Errorf("insert provider model: %w", err)
		}
	}

	if _, err = tx.ExecContext(ctx, `
		UPDATE providers
		SET last_error = NULL, last_synced_at = ?, updated_at = ?
		WHERE id = ?
	`, insertedAt, insertedAt, id); err != nil {
		return fmt.Errorf("update sync metadata: %w", err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit model sync: %w", err)
	}

	return nil
}

// setProviderSyncError stores the latest catalog sync failure for the provider.
func (s *store) setProviderSyncError(ctx context.Context, id int64, syncErr error) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE providers
		SET last_error = ?, updated_at = ?
		WHERE id = ?
	`, syncErr.Error(), nowRFC3339(), id)
	if err != nil {
		return fmt.Errorf("persist sync error: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read sync error rows: %w", err)
	}
	if affected == 0 {
		return errProviderNotFound
	}

	return nil
}

// listRouteModels returns the aggregated routable models and their enabled providers.
func (s *store) listRouteModels(ctx context.Context) ([]routeModelView, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT pm.model_id, p.id, p.name
		FROM provider_models pm
		INNER JOIN providers p ON p.id = pm.provider_id
		WHERE p.enabled = 1
		ORDER BY pm.model_id ASC, p.name ASC, p.id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query route models: %w", err)
	}
	defer rows.Close()

	modelIndex := make(map[string]int)
	models := make([]routeModelView, 0)
	for rows.Next() {
		var modelID string
		var providerID int64
		var providerName string
		if err := rows.Scan(&modelID, &providerID, &providerName); err != nil {
			return nil, fmt.Errorf("scan route model: %w", err)
		}

		modelPos, found := modelIndex[modelID]
		if !found {
			modelPos = len(models)
			modelIndex[modelID] = modelPos
			models = append(models, routeModelView{
				ID:        modelID,
				Providers: make([]routeProviderView, 0, 1),
			})
		}

		models[modelPos].Providers = append(models[modelPos].Providers, routeProviderView{
			ID:   providerID,
			Name: providerName,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate route models: %w", err)
	}

	return models, nil
}

// findProviderForModel returns the most recent enabled provider that serves a model.
func (s *store) findProviderForModel(ctx context.Context, modelID string) (providerRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.base_url, p.api_key, p.user_agent, p.enabled, p.last_error, p.last_synced_at, p.created_at, p.updated_at
		FROM providers p
		INNER JOIN provider_models pm ON pm.provider_id = p.id
		WHERE p.enabled = 1 AND pm.model_id = ?
		ORDER BY CASE WHEN p.last_synced_at IS NULL THEN 1 ELSE 0 END, p.last_synced_at DESC, p.id ASC
		LIMIT 1
	`, modelID)

	record, err := scanProvider(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return providerRecord{}, errProviderNotFound
		}
		return providerRecord{}, err
	}

	record.Models = []string{modelID}
	return record, nil
}

// attachModels populates the model lists for a batch of provider records.
func (s *store) attachModels(ctx context.Context, providers []providerRecord) error {
	if len(providers) == 0 {
		return nil
	}

	providerModels := make(map[int64][]string, len(providers))
	for _, provider := range providers {
		models, err := s.listModelsForProvider(ctx, provider.ID)
		if err != nil {
			return err
		}
		providerModels[provider.ID] = models
	}

	for index := range providers {
		providers[index].Models = providerModels[providers[index].ID]
	}

	return nil
}

// listModelsForProvider returns the sorted model IDs stored for one provider.
func (s *store) listModelsForProvider(ctx context.Context, providerID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT model_id
		FROM provider_models
		WHERE provider_id = ?
		ORDER BY model_id ASC
	`, providerID)
	if err != nil {
		return nil, fmt.Errorf("query provider models: %w", err)
	}
	defer rows.Close()

	models := make([]string, 0)
	for rows.Next() {
		var modelID string
		if err := rows.Scan(&modelID); err != nil {
			return nil, fmt.Errorf("scan provider model: %w", err)
		}
		models = append(models, modelID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider models: %w", err)
	}

	return models, nil
}

// scanProxyRequestLog reads a proxy request audit row from either a query row or iterator.
func scanProxyRequestLog(scanner interface {
	Scan(dest ...any) error
}) (proxyRequestLogRecord, error) {
	var record proxyRequestLogRecord
	var (
		requestBodyTruncated  int
		sentBodyTruncated     int
		responseStatus        sql.NullInt64
		responseBodyTruncated int
		durationMS            sql.NullInt64
		requestedAtRaw        string
		completedAtRaw        sql.NullString
	)

	if err := scanner.Scan(
		&record.ID,
		&record.ProviderID,
		&record.ProviderName,
		&record.ModelID,
		&record.Method,
		&record.Path,
		&record.RawQuery,
		&record.RequestHeaders,
		&record.RequestBody,
		&requestBodyTruncated,
		&record.SentMethod,
		&record.SentURL,
		&record.SentHeaders,
		&record.SentBody,
		&sentBodyTruncated,
		&responseStatus,
		&record.ResponseHeaders,
		&record.ResponseBody,
		&responseBodyTruncated,
		&record.ErrorText,
		&durationMS,
		&requestedAtRaw,
		&completedAtRaw,
	); err != nil {
		return proxyRequestLogRecord{}, err
	}

	record.RequestBodyTruncated = requestBodyTruncated == 1
	record.SentBodyTruncated = sentBodyTruncated == 1
	record.ResponseStatus = responseStatus
	record.ResponseBodyTruncated = responseBodyTruncated == 1
	record.DurationMS = durationMS

	requestedAt, err := time.Parse(time.RFC3339, requestedAtRaw)
	if err != nil {
		return proxyRequestLogRecord{}, fmt.Errorf("parse proxy request requested_at: %w", err)
	}
	record.RequestedAt = requestedAt

	if completedAtRaw.Valid {
		parsed, err := time.Parse(time.RFC3339, completedAtRaw.String)
		if err != nil {
			return proxyRequestLogRecord{}, fmt.Errorf("parse proxy request completed_at: %w", err)
		}
		record.CompletedAt = sql.NullTime{Time: parsed, Valid: true}
	}

	return record, nil
}

// scanProvider reads a provider row from either a query row or rows iterator.
func scanProvider(scanner interface {
	Scan(dest ...any) error
}) (providerRecord, error) {
	var (
		record       providerRecord
		enabled      int
		lastError    sql.NullString
		lastSyncedAt sql.NullString
		createdAtRaw string
		updatedAtRaw string
	)

	if err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.BaseURL,
		&record.APIKey,
		&record.UserAgent,
		&enabled,
		&lastError,
		&lastSyncedAt,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		return providerRecord{}, err
	}

	record.Enabled = enabled == 1
	record.LastError = lastError.String

	createdAt, err := time.Parse(time.RFC3339, createdAtRaw)
	if err != nil {
		return providerRecord{}, fmt.Errorf("parse provider created_at: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339, updatedAtRaw)
	if err != nil {
		return providerRecord{}, fmt.Errorf("parse provider updated_at: %w", err)
	}
	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt

	if lastSyncedAt.Valid {
		parsed, err := time.Parse(time.RFC3339, lastSyncedAt.String)
		if err != nil {
			return providerRecord{}, fmt.Errorf("parse provider last_synced_at: %w", err)
		}
		record.LastSyncedAt = &parsed
	}

	return record, nil
}

// toView converts the internal provider record into the redacted UI payload.
func (p providerRecord) toView() providerView {
	view := providerView{
		ID:               p.ID,
		Name:             p.Name,
		BaseURL:          p.BaseURL,
		UserAgent:        p.UserAgent,
		Enabled:          p.Enabled,
		Models:           append([]string(nil), p.Models...),
		LastError:        p.LastError,
		APIKeyConfigured: p.APIKey != "",
		APIKeyPreview:    maskAPIKey(p.APIKey),
	}
	if p.LastSyncedAt != nil {
		view.LastSyncedAt = p.LastSyncedAt.UTC().Format(time.RFC3339)
	}
	return view
}

// toView converts the internal audit record into the JSON payload used by the UI.
func (record proxyRequestLogRecord) toView() proxyRequestLogView {
	view := proxyRequestLogView{
		ID:           record.ID,
		ProviderName: record.ProviderName,
		ModelID:      record.ModelID,
		ReceivedRequest: proxyRequestReceivedRequestView{
			Method:        record.Method,
			Path:          record.Path,
			RawQuery:      record.RawQuery,
			Headers:       record.RequestHeaders,
			Body:          record.RequestBody,
			BodyTruncated: record.RequestBodyTruncated,
		},
		ReceivedResponse: proxyRequestReceivedResponseView{
			Headers:       record.ResponseHeaders,
			Body:          record.ResponseBody,
			BodyTruncated: record.ResponseBodyTruncated,
			Error:         record.ErrorText,
		},
		RequestedAt: record.RequestedAt.UTC().Format(time.RFC3339),
	}
	if record.ProviderID.Valid {
		view.ProviderID = record.ProviderID.Int64
	}
	if record.SentMethod != "" || record.SentURL != "" || record.SentHeaders != "" || record.SentBody != "" || record.SentBodyTruncated {
		view.SentRequest = &proxyRequestSentRequestView{
			Method:        record.SentMethod,
			URL:           record.SentURL,
			Headers:       record.SentHeaders,
			Body:          record.SentBody,
			BodyTruncated: record.SentBodyTruncated,
		}
	}
	if record.ResponseStatus.Valid {
		view.ReceivedResponse.Status = int(record.ResponseStatus.Int64)
	}
	if record.DurationMS.Valid {
		view.DurationMS = record.DurationMS.Int64
	}
	if record.CompletedAt.Valid {
		view.CompletedAt = record.CompletedAt.Time.UTC().Format(time.RFC3339)
	}

	return view
}

// maskAPIKey keeps only a short prefix and suffix so the UI can show a safe preview.
func maskAPIKey(apiKey string) string {
	if apiKey == "" {
		return ""
	}
	if len(apiKey) <= 8 {
		return strings.Repeat("*", len(apiKey))
	}
	return fmt.Sprintf("%s...%s", apiKey[:4], apiKey[len(apiKey)-4:])
}

// boolToInt converts a boolean flag into SQLite's integer representation.
func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// nullableInt64 converts an optional provider identifier into a database value.
func nullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

// nowRFC3339 returns the current UTC timestamp formatted for SQLite storage.
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// uniqueStrings trims, deduplicates, and sorts a list of model IDs.
func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	deduped := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, found := seen[trimmed]; found {
			continue
		}
		seen[trimmed] = struct{}{}
		deduped = append(deduped, trimmed)
	}
	sort.Strings(deduped)
	return deduped
}
