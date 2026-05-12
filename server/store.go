package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"sort"
	"strings"
	"time"
)

var (
	errProviderNotFound      = errors.New("provider not found")
	errProviderModelNotFound = errors.New("provider does not serve model")
	errModelAliasNotFound    = errors.New("model alias not found")
	errModelAliasConflict    = errors.New("model alias conflict")
	errModelAliasTarget      = errors.New("model alias target is not routable")
)

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
	// DisabledModels contains the synced model IDs that are currently disabled for this provider.
	DisabledModels []string
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
	// DisabledModels lists the synced model IDs currently blocked by disable rules.
	DisabledModels []string `json:"disabledModels"`
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

// modelAliasRecord is the internal database representation of one configured
// alias model and the routable target it resolves to.
type modelAliasRecord struct {
	// ID is the database primary key for the alias row.
	ID int64
	// AliasModelID is the new public model name exposed by the gateway.
	AliasModelID string
	// TargetModelID is the upstream model name that requests should use.
	TargetModelID string
	// TargetProviderID optionally pins the alias to one provider.
	TargetProviderID sql.NullInt64
	// TargetProviderName stores the pinned provider's display name when the
	// record is read through a provider join.
	TargetProviderName string
	// CreatedAt records when the alias row was inserted.
	CreatedAt time.Time
	// UpdatedAt records when the alias row was last modified.
	UpdatedAt time.Time
}

// modelAliasMutation captures the normalized fields used when creating or
// updating a model alias.
type modelAliasMutation struct {
	// AliasModelID is the new public model name exposed by the gateway.
	AliasModelID string
	// TargetModelID is the upstream model name that requests should use.
	TargetModelID string
	// TargetProviderID optionally pins the alias to one provider.
	TargetProviderID *int64
}

// modelDisableRuleMutation captures one desired final provider/model disable
// state after the admin API request completes.
type modelDisableRuleMutation struct {
	// ProviderID identifies the provider affected by the rule.
	ProviderID int64
	// ModelID identifies the synced model affected by the rule.
	ModelID string
	// Disabled reports whether the rule should exist after the request completes.
	Disabled bool
}

// modelAliasView is the JSON payload returned to the Web UI for configured
// model aliases.
type modelAliasView struct {
	// ID is the alias identifier.
	ID int64 `json:"id"`
	// AliasModelID is the new public model name exposed by the gateway.
	AliasModelID string `json:"aliasModelId"`
	// TargetModelID is the upstream model name that requests should use.
	TargetModelID string `json:"targetModelId"`
	// TargetProviderID optionally pins the alias to one provider.
	TargetProviderID int64 `json:"targetProviderId,omitempty"`
	// TargetProviderName is the pinned provider label when one is configured.
	TargetProviderName string `json:"targetProviderName,omitempty"`
	// Providers lists the enabled providers that currently make the alias
	// routable after disable rules are applied.
	Providers []routeProviderView `json:"providers"`
	// Routable reports whether the alias currently resolves to at least one
	// enabled provider route.
	Routable bool `json:"routable"`
	// CreatedAt records when the alias row was inserted.
	CreatedAt string `json:"createdAt"`
	// UpdatedAt records when the alias row was last modified.
	UpdatedAt string `json:"updatedAt"`
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

// requestStatsSummaryView is the aggregated request-and-token summary returned
// by the stats API for the selected date range.
type requestStatsSummaryView struct {
	// Requests is the total number of audited requests in the selected range.
	Requests int64 `json:"requests"`
	// Succeeded is the number of completed requests that returned a 2xx status.
	Succeeded int64 `json:"succeeded"`
	// Failed is the number of completed requests that returned a non-2xx status.
	Failed int64 `json:"failed"`
	// ConsumedTokens is the total of input plus output tokens.
	ConsumedTokens int64 `json:"consumedTokens"`
	// CachedInputTokens is the subset of input tokens served from cache.
	CachedInputTokens int64 `json:"cachedInputTokens"`
	// NonCachedInputTokens is the subset of input tokens that were not cached.
	NonCachedInputTokens int64 `json:"nonCachedInputTokens"`
	// OutputTokens is the number of generated output tokens.
	OutputTokens int64 `json:"outputTokens"`
	// OngoingRequests is the number of requests that have not completed yet.
	OngoingRequests int64 `json:"ongoingRequests"`
}

// requestStatsBucketView is one bucket in the token-usage charts returned by
// the stats API.
type requestStatsBucketView struct {
	// Start is the UTC bucket start timestamp.
	Start string `json:"start"`
	// Label is the human-readable label shown in the chart.
	Label string `json:"label"`
	// Requests is the number of requests started within the bucket.
	Requests int64 `json:"requests"`
	// Succeeded is the number of completed 2xx requests in the bucket.
	Succeeded int64 `json:"succeeded"`
	// Failed is the number of completed non-2xx requests in the bucket.
	Failed int64 `json:"failed"`
	// ConsumedTokens is the sum of input and output tokens for the bucket.
	ConsumedTokens int64 `json:"consumedTokens"`
	// CachedInputTokens is the number of cached input tokens in the bucket.
	CachedInputTokens int64 `json:"cachedInputTokens"`
	// NonCachedInputTokens is the number of non-cached input tokens.
	NonCachedInputTokens int64 `json:"nonCachedInputTokens"`
	// OutputTokens is the number of output tokens in the bucket.
	OutputTokens int64 `json:"outputTokens"`
}

// requestStatsView is the JSON payload returned by the stats API.
type requestStatsView struct {
	// Range is the canonical selector used for the current summary window.
	Range string `json:"range"`
	// RangeLabel is the human-readable label for the current summary window.
	RangeLabel string `json:"rangeLabel"`
	// Summary contains the top-line counts for the selected range.
	Summary requestStatsSummaryView `json:"summary"`
	// Daily contains the recent 7-day token chart.
	Daily []requestStatsBucketView `json:"daily"`
	// Hourly contains the recent 12-hour token chart.
	Hourly []requestStatsBucketView `json:"hourly"`
}

// requestTokenUsage captures the token counts extracted from a completed
// OpenAI-style response body.
type requestTokenUsage struct {
	// InputTokens is the number of prompt or input tokens reported by the API.
	InputTokens int64
	// CachedInputTokens is the portion of input tokens served from cache.
	CachedInputTokens int64
	// NonCachedInputTokens is the portion of input tokens not served from cache.
	NonCachedInputTokens int64
	// OutputTokens is the number of generated output tokens reported by the API.
	OutputTokens int64
}

// requestStatsBucketFrame tracks the time span and running totals for one chart bucket.
type requestStatsBucketFrame struct {
	// Start marks the inclusive start of the bucket.
	Start time.Time
	// End marks the exclusive end of the bucket.
	End time.Time
	// Label is the human-readable label shown in the chart.
	Label string
	// Requests counts requests that began in this bucket.
	Requests int64
	// Succeeded counts successful requests in this bucket.
	Succeeded int64
	// Failed counts failed requests in this bucket.
	Failed int64
	// ConsumedTokens is the sum of input and output tokens for the bucket.
	ConsumedTokens int64
	// CachedInputTokens counts cached input tokens for the bucket.
	CachedInputTokens int64
	// NonCachedInputTokens counts non-cached input tokens for the bucket.
	NonCachedInputTokens int64
	// OutputTokens counts output tokens for the bucket.
	OutputTokens int64
}

// requestStatsWindow captures the selected summary range for the stats API.
type requestStatsWindow struct {
	// Key is the canonical selector used by the UI.
	Key string
	// Label is the human-readable label shown in the UI.
	Label string
	// From is the optional inclusive lower bound for the selected range.
	From *time.Time
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
		`CREATE TABLE IF NOT EXISTS provider_model_disable_rules (
			provider_id INTEGER NOT NULL,
			model_id TEXT NOT NULL,
			created_at TEXT NOT NULL,
			PRIMARY KEY (provider_id, model_id),
			FOREIGN KEY (provider_id) REFERENCES providers(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_provider_model_disable_rules_model_id ON provider_model_disable_rules (model_id, provider_id);`,
		`CREATE TABLE IF NOT EXISTS model_aliases (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			alias_model_id TEXT NOT NULL UNIQUE,
			target_model_id TEXT NOT NULL,
			target_provider_id INTEGER,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY (target_provider_id) REFERENCES providers(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_model_aliases_target_model_id ON model_aliases (target_model_id);`,
		`CREATE INDEX IF NOT EXISTS idx_model_aliases_target_provider_id ON model_aliases (target_provider_id, target_model_id);`,
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
		`CREATE INDEX IF NOT EXISTS idx_proxy_request_logs_completed_at ON proxy_request_logs (completed_at);`,
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
	if err := s.ensureAuthTables(ctx); err != nil {
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

	disabledModels, err := s.listDisabledModelsForProvider(ctx, id)
	if err != nil {
		return providerRecord{}, err
	}
	record.DisabledModels = disabledModels

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

// setProviderModelDisabledBatch creates or removes multiple provider/model
// disable rules in one transaction so the UI can apply a staged set atomically.
func (s *store) setProviderModelDisabledBatch(ctx context.Context, mutations []modelDisableRuleMutation) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin model disable rule transaction: %w", err)
	}

	for _, mutation := range mutations {
		if err := s.setProviderModelDisabledTx(ctx, tx, mutation); err != nil {
			_ = tx.Rollback()
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit model disable rule transaction: %w", err)
	}

	return nil
}

// setProviderModelDisabledTx validates one provider/model pair and applies the
// requested disable-rule state using the supplied transaction.
func (s *store) setProviderModelDisabledTx(ctx context.Context, tx *sql.Tx, mutation modelDisableRuleMutation) error {
	exists, err := s.providerExistsTx(ctx, tx, mutation.ProviderID)
	if err != nil {
		return err
	}
	if !exists {
		return errProviderNotFound
	}

	if mutation.Disabled {
		servesModel, err := s.providerServesModelTx(ctx, tx, mutation.ProviderID, mutation.ModelID)
		if err != nil {
			return err
		}
		if !servesModel {
			return errProviderModelNotFound
		}

		if _, err := tx.ExecContext(ctx, `
			INSERT INTO provider_model_disable_rules (provider_id, model_id, created_at)
			VALUES (?, ?, ?)
			ON CONFLICT(provider_id, model_id) DO NOTHING
		`, mutation.ProviderID, mutation.ModelID, nowRFC3339()); err != nil {
			return fmt.Errorf("insert provider model disable rule: %w", err)
		}

		return nil
	}

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM provider_model_disable_rules
		WHERE provider_id = ? AND model_id = ?
	`, mutation.ProviderID, mutation.ModelID); err != nil {
		return fmt.Errorf("delete provider model disable rule: %w", err)
	}

	return nil
}

// providerExistsTx reports whether the referenced provider row exists inside
// the supplied transaction scope.
func (s *store) providerExistsTx(ctx context.Context, tx *sql.Tx, providerID int64) (bool, error) {
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT 1 FROM providers WHERE id = ?`, providerID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("query provider: %w", err)
	}

	return true, nil
}

// providerServesModelTx reports whether the referenced provider currently has
// a synced mapping for the requested model inside the supplied transaction.
func (s *store) providerServesModelTx(ctx context.Context, tx *sql.Tx, providerID int64, modelID string) (bool, error) {
	var exists int
	if err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM provider_models
		WHERE provider_id = ? AND model_id = ?
	`, providerID, modelID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("query provider model mapping: %w", err)
	}

	return true, nil
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

// listProxyRequestLogsBetween returns audited request rows whose requested-at
// timestamp falls within the provided inclusive range.
func (s *store) listProxyRequestLogsBetween(ctx context.Context, from *time.Time, to time.Time) ([]proxyRequestLogRecord, error) {
	clauses := []string{"requested_at <= ?"}
	args := []any{to.UTC().Format(time.RFC3339)}
	if from != nil {
		clauses = append(clauses, "requested_at >= ?")
		args = append(args, from.UTC().Format(time.RFC3339))
	}

	query := fmt.Sprintf(`
		SELECT
			id, provider_id, provider_name, model_id, method, path, raw_query,
			request_headers, request_body, request_body_truncated,
			sent_request_method, sent_request_url, sent_request_headers, sent_request_body, sent_request_body_truncated,
			response_status, response_headers, response_body, response_body_truncated,
			error_text, duration_ms, requested_at, completed_at
		FROM proxy_request_logs
		WHERE %s
		ORDER BY requested_at ASC, id ASC
	`, strings.Join(clauses, " AND "))

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query proxy request logs by range: %w", err)
	}
	defer rows.Close()

	logs := make([]proxyRequestLogRecord, 0)
	for rows.Next() {
		record, err := scanProxyRequestLog(rows)
		if err != nil {
			return nil, err
		}
		logs = append(logs, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate proxy request logs by range: %w", err)
	}

	return logs, nil
}

// countOngoingProxyRequestLogs returns the number of audited requests whose
// response has not finished yet.
func (s *store) countOngoingProxyRequestLogs(ctx context.Context) (int64, error) {
	var count int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM proxy_request_logs
		WHERE completed_at IS NULL
	`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count ongoing proxy request logs: %w", err)
	}

	return count, nil
}

// listRequestStats returns the selected summary metrics together with the fixed
// recent daily and hourly token-usage charts for the dashboard.
func (s *store) listRequestStats(ctx context.Context, window requestStatsWindow, now time.Time) (requestStatsView, error) {
	summaryLogs, err := s.listProxyRequestLogsBetween(ctx, window.From, now)
	if err != nil {
		return requestStatsView{}, err
	}

	summary := summarizeRequestLogs(summaryLogs)
	ongoing, err := s.countOngoingProxyRequestLogs(ctx)
	if err != nil {
		return requestStatsView{}, err
	}
	summary.OngoingRequests = ongoing

	dailyStart := time.Date(now.UTC().Year(), now.UTC().Month(), now.UTC().Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -6)
	dailyLogs, err := s.listProxyRequestLogsBetween(ctx, &dailyStart, now)
	if err != nil {
		return requestStatsView{}, err
	}

	hourlyStart := now.UTC().Truncate(time.Hour).Add(-11 * time.Hour)
	hourlyLogs, err := s.listProxyRequestLogsBetween(ctx, &hourlyStart, now)
	if err != nil {
		return requestStatsView{}, err
	}

	return requestStatsView{
		Range:      window.Key,
		RangeLabel: window.Label,
		Summary:    summary,
		Daily:      buildDailyRequestStatsBuckets(dailyLogs, dailyStart),
		Hourly:     buildHourlyRequestStatsBuckets(hourlyLogs, hourlyStart),
	}, nil
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
		DELETE FROM provider_model_disable_rules
		WHERE provider_id = ? AND model_id NOT IN (
			SELECT model_id
			FROM provider_models
			WHERE provider_id = ?
		)
	`, id, id); err != nil {
		return fmt.Errorf("clear stale provider model disable rules: %w", err)
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
		LEFT JOIN provider_model_disable_rules dr ON dr.provider_id = pm.provider_id AND dr.model_id = pm.model_id
		WHERE p.enabled = 1 AND dr.provider_id IS NULL
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

// listPublicRouteModels returns the aggregated provider-backed models together
// with any configured alias models that currently resolve to at least one route.
func (s *store) listPublicRouteModels(ctx context.Context) ([]routeModelView, error) {
	models, err := s.listRouteModels(ctx)
	if err != nil {
		return nil, err
	}

	modelIndex := make(map[string]int, len(models))
	for index := range models {
		modelIndex[models[index].ID] = index
	}

	aliases, err := s.listModelAliases(ctx)
	if err != nil {
		return nil, err
	}

	for _, alias := range aliases {
		providers, err := s.listProvidersForAlias(ctx, alias)
		if err != nil {
			return nil, err
		}
		if len(providers) == 0 {
			continue
		}
		if _, found := modelIndex[alias.AliasModelID]; found {
			continue
		}

		models = append(models, routeModelView{
			ID:        alias.AliasModelID,
			Providers: providers,
		})
	}

	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})

	return models, nil
}

// findProviderForModel returns the most recent enabled provider that serves a model.
func (s *store) findProviderForModel(ctx context.Context, modelID string) (providerRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.base_url, p.api_key, p.user_agent, p.enabled, p.last_error, p.last_synced_at, p.created_at, p.updated_at
		FROM providers p
		INNER JOIN provider_models pm ON pm.provider_id = p.id
		LEFT JOIN provider_model_disable_rules dr ON dr.provider_id = pm.provider_id AND dr.model_id = pm.model_id
		WHERE p.enabled = 1 AND pm.model_id = ? AND dr.provider_id IS NULL
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

// listModelAliases returns every configured alias model ordered by alias name.
func (s *store) listModelAliases(ctx context.Context) ([]modelAliasRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT ma.id, ma.alias_model_id, ma.target_model_id, ma.target_provider_id, p.name, ma.created_at, ma.updated_at
		FROM model_aliases ma
		LEFT JOIN providers p ON p.id = ma.target_provider_id
		ORDER BY ma.alias_model_id ASC, ma.id ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("query model aliases: %w", err)
	}
	defer rows.Close()

	aliases := make([]modelAliasRecord, 0)
	for rows.Next() {
		record, err := scanModelAlias(rows)
		if err != nil {
			return nil, err
		}
		aliases = append(aliases, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model aliases: %w", err)
	}

	return aliases, nil
}

// listModelAliasViews returns the configured aliases together with their
// currently routable provider summaries.
func (s *store) listModelAliasViews(ctx context.Context) ([]modelAliasView, error) {
	aliases, err := s.listModelAliases(ctx)
	if err != nil {
		return nil, err
	}

	views := make([]modelAliasView, 0, len(aliases))
	for _, alias := range aliases {
		providers, err := s.listProvidersForAlias(ctx, alias)
		if err != nil {
			return nil, err
		}
		views = append(views, alias.toView(providers))
	}

	return views, nil
}

// getModelAlias retrieves one alias row by its database identifier.
func (s *store) getModelAlias(ctx context.Context, id int64) (modelAliasRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT ma.id, ma.alias_model_id, ma.target_model_id, ma.target_provider_id, p.name, ma.created_at, ma.updated_at
		FROM model_aliases ma
		LEFT JOIN providers p ON p.id = ma.target_provider_id
		WHERE ma.id = ?
	`, id)

	record, err := scanModelAlias(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return modelAliasRecord{}, errModelAliasNotFound
		}
		return modelAliasRecord{}, err
	}

	return record, nil
}

// getModelAliasByName retrieves one alias row by its public model identifier.
func (s *store) getModelAliasByName(ctx context.Context, aliasModelID string) (modelAliasRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT ma.id, ma.alias_model_id, ma.target_model_id, ma.target_provider_id, p.name, ma.created_at, ma.updated_at
		FROM model_aliases ma
		LEFT JOIN providers p ON p.id = ma.target_provider_id
		WHERE ma.alias_model_id = ?
	`, aliasModelID)

	record, err := scanModelAlias(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return modelAliasRecord{}, errModelAliasNotFound
		}
		return modelAliasRecord{}, err
	}

	return record, nil
}

// createModelAlias inserts a new alias row after validating the configured
// target route and ensuring the alias name does not collide with a routable model.
func (s *store) createModelAlias(ctx context.Context, mutation modelAliasMutation) (modelAliasRecord, error) {
	if err := s.validateModelAliasMutation(ctx, mutation, nil); err != nil {
		return modelAliasRecord{}, err
	}

	now := nowRFC3339()
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO model_aliases (alias_model_id, target_model_id, target_provider_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
	`, mutation.AliasModelID, mutation.TargetModelID, nullableInt64(mutation.TargetProviderID), now, now)
	if err != nil {
		return modelAliasRecord{}, fmt.Errorf("insert model alias: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return modelAliasRecord{}, fmt.Errorf("read model alias id: %w", err)
	}

	return s.getModelAlias(ctx, id)
}

// updateModelAlias updates an existing alias row and revalidates the requested
// route target before persisting the new configuration.
func (s *store) updateModelAlias(ctx context.Context, id int64, mutation modelAliasMutation) (modelAliasRecord, error) {
	if _, err := s.getModelAlias(ctx, id); err != nil {
		return modelAliasRecord{}, err
	}
	if err := s.validateModelAliasMutation(ctx, mutation, &id); err != nil {
		return modelAliasRecord{}, err
	}

	result, err := s.db.ExecContext(ctx, `
		UPDATE model_aliases
		SET alias_model_id = ?, target_model_id = ?, target_provider_id = ?, updated_at = ?
		WHERE id = ?
	`, mutation.AliasModelID, mutation.TargetModelID, nullableInt64(mutation.TargetProviderID), nowRFC3339(), id)
	if err != nil {
		return modelAliasRecord{}, fmt.Errorf("update model alias: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return modelAliasRecord{}, fmt.Errorf("read updated model alias rows: %w", err)
	}
	if affected == 0 {
		return modelAliasRecord{}, errModelAliasNotFound
	}

	return s.getModelAlias(ctx, id)
}

// deleteModelAlias removes one alias row from the database.
func (s *store) deleteModelAlias(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM model_aliases WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete model alias: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted model alias rows: %w", err)
	}
	if affected == 0 {
		return errModelAliasNotFound
	}

	return nil
}

// listProvidersForAlias returns the enabled provider routes that currently make
// an alias routable.
func (s *store) listProvidersForAlias(ctx context.Context, alias modelAliasRecord) ([]routeProviderView, error) {
	if alias.TargetProviderID.Valid {
		provider, err := s.getRoutableProviderForModel(ctx, alias.TargetProviderID.Int64, alias.TargetModelID)
		if err != nil {
			if errors.Is(err, errProviderNotFound) {
				return nil, nil
			}
			return nil, err
		}
		return []routeProviderView{{ID: provider.ID, Name: provider.Name}}, nil
	}

	return s.listProvidersForModel(ctx, alias.TargetModelID)
}

// listProvidersForModel returns the enabled provider summaries that can route a
// model after applying model disable rules.
func (s *store) listProvidersForModel(ctx context.Context, modelID string) ([]routeProviderView, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id, p.name
		FROM provider_models pm
		INNER JOIN providers p ON p.id = pm.provider_id
		LEFT JOIN provider_model_disable_rules dr ON dr.provider_id = pm.provider_id AND dr.model_id = pm.model_id
		WHERE p.enabled = 1 AND pm.model_id = ? AND dr.provider_id IS NULL
		ORDER BY p.name ASC, p.id ASC
	`, modelID)
	if err != nil {
		return nil, fmt.Errorf("query route providers: %w", err)
	}
	defer rows.Close()

	providers := make([]routeProviderView, 0)
	for rows.Next() {
		var providerID int64
		var providerName string
		if err := rows.Scan(&providerID, &providerName); err != nil {
			return nil, fmt.Errorf("scan route provider: %w", err)
		}
		providers = append(providers, routeProviderView{ID: providerID, Name: providerName})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate route providers: %w", err)
	}

	return providers, nil
}

// getRoutableProviderForModel returns one specific provider only when it can
// currently route the requested model.
func (s *store) getRoutableProviderForModel(ctx context.Context, providerID int64, modelID string) (providerRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT p.id, p.name, p.base_url, p.api_key, p.user_agent, p.enabled, p.last_error, p.last_synced_at, p.created_at, p.updated_at
		FROM providers p
		INNER JOIN provider_models pm ON pm.provider_id = p.id
		LEFT JOIN provider_model_disable_rules dr ON dr.provider_id = pm.provider_id AND dr.model_id = pm.model_id
		WHERE p.id = ? AND p.enabled = 1 AND pm.model_id = ? AND dr.provider_id IS NULL
		LIMIT 1
	`, providerID, modelID)

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

// validateModelAliasMutation checks alias collisions and ensures the target
// route is currently usable before the alias is persisted.
func (s *store) validateModelAliasMutation(ctx context.Context, mutation modelAliasMutation, excludeID *int64) error {
	mutation.AliasModelID = strings.TrimSpace(mutation.AliasModelID)
	mutation.TargetModelID = strings.TrimSpace(mutation.TargetModelID)
	if mutation.AliasModelID == "" {
		return errors.New("alias model id is required")
	}
	if mutation.TargetModelID == "" {
		return errors.New("target model id is required")
	}
	if mutation.AliasModelID == mutation.TargetModelID {
		return errors.New("alias model id must differ from target model id")
	}

	if collision, err := s.modelAliasConflicts(ctx, mutation.AliasModelID, excludeID); err != nil {
		return err
	} else if collision {
		return fmt.Errorf("%w: alias model %q conflicts with an existing routable model", errModelAliasConflict, mutation.AliasModelID)
	}

	if mutation.TargetProviderID != nil {
		if _, err := s.getRoutableProviderForModel(ctx, *mutation.TargetProviderID, mutation.TargetModelID); err != nil {
			if errors.Is(err, errProviderNotFound) {
				return fmt.Errorf("%w: provider %d does not route model %q", errModelAliasTarget, *mutation.TargetProviderID, mutation.TargetModelID)
			}
			return err
		}
		return nil
	}

	if provider, err := s.findProviderForModel(ctx, mutation.TargetModelID); err != nil {
		if errors.Is(err, errProviderNotFound) {
			return fmt.Errorf("%w: model %q is not routed by any enabled provider", errModelAliasTarget, mutation.TargetModelID)
		}
		return err
	} else if provider.ID <= 0 {
		return fmt.Errorf("%w: model %q is not routed by any enabled provider", errModelAliasTarget, mutation.TargetModelID)
	}

	return nil
}

// modelAliasConflicts reports whether the alias name collides with a routed
// provider model or another alias row.
func (s *store) modelAliasConflicts(ctx context.Context, aliasModelID string, excludeID *int64) (bool, error) {
	var providerCount int
	if err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(1)
		FROM provider_models
		WHERE model_id = ?
	`, aliasModelID).Scan(&providerCount); err != nil {
		return false, fmt.Errorf("query alias model conflicts: %w", err)
	}
	if providerCount > 0 {
		return true, nil
	}

	query := `SELECT COUNT(1) FROM model_aliases WHERE alias_model_id = ?`
	args := []any{aliasModelID}
	if excludeID != nil {
		query += " AND id <> ?"
		args = append(args, *excludeID)
	}

	var aliasCount int
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&aliasCount); err != nil {
		return false, fmt.Errorf("query existing model aliases: %w", err)
	}

	return aliasCount > 0, nil
}

// attachModels populates the model lists and disabled-model lists for a batch
// of provider records.
func (s *store) attachModels(ctx context.Context, providers []providerRecord) error {
	if len(providers) == 0 {
		return nil
	}

	providerModels := make(map[int64][]string, len(providers))
	providerDisabledModels := make(map[int64][]string, len(providers))
	for _, provider := range providers {
		models, err := s.listModelsForProvider(ctx, provider.ID)
		if err != nil {
			return err
		}
		providerModels[provider.ID] = models

		disabledModels, err := s.listDisabledModelsForProvider(ctx, provider.ID)
		if err != nil {
			return err
		}
		providerDisabledModels[provider.ID] = disabledModels
	}

	for index := range providers {
		providers[index].Models = providerModels[providers[index].ID]
		providers[index].DisabledModels = providerDisabledModels[providers[index].ID]
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

// listDisabledModelsForProvider returns the sorted model IDs blocked by
// disable rules for one provider.
func (s *store) listDisabledModelsForProvider(ctx context.Context, providerID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT model_id
		FROM provider_model_disable_rules
		WHERE provider_id = ?
		ORDER BY model_id ASC
	`, providerID)
	if err != nil {
		return nil, fmt.Errorf("query provider model disable rules: %w", err)
	}
	defer rows.Close()

	modelIDs := make([]string, 0)
	for rows.Next() {
		var modelID string
		if err := rows.Scan(&modelID); err != nil {
			return nil, fmt.Errorf("scan provider model disable rule: %w", err)
		}
		modelIDs = append(modelIDs, modelID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider model disable rules: %w", err)
	}

	return modelIDs, nil
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

// scanModelAlias reads an alias row from either a query row or rows iterator.
func scanModelAlias(scanner interface {
	Scan(dest ...any) error
}) (modelAliasRecord, error) {
	var (
		record             modelAliasRecord
		targetProviderName sql.NullString
		createdAtRaw       string
		updatedAtRaw       string
	)

	if err := scanner.Scan(
		&record.ID,
		&record.AliasModelID,
		&record.TargetModelID,
		&record.TargetProviderID,
		&targetProviderName,
		&createdAtRaw,
		&updatedAtRaw,
	); err != nil {
		return modelAliasRecord{}, err
	}

	record.TargetProviderName = targetProviderName.String

	createdAt, err := time.Parse(time.RFC3339, createdAtRaw)
	if err != nil {
		return modelAliasRecord{}, fmt.Errorf("parse model alias created_at: %w", err)
	}
	updatedAt, err := time.Parse(time.RFC3339, updatedAtRaw)
	if err != nil {
		return modelAliasRecord{}, fmt.Errorf("parse model alias updated_at: %w", err)
	}
	record.CreatedAt = createdAt
	record.UpdatedAt = updatedAt

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
		Models:           append([]string{}, p.Models...),
		DisabledModels:   append([]string{}, p.DisabledModels...),
		LastError:        p.LastError,
		APIKeyConfigured: p.APIKey != "",
		APIKeyPreview:    maskAPIKey(p.APIKey),
	}
	if p.LastSyncedAt != nil {
		view.LastSyncedAt = p.LastSyncedAt.UTC().Format(time.RFC3339)
	}
	return view
}

// toView converts the internal alias record into the JSON payload used by the UI.
func (record modelAliasRecord) toView(providers []routeProviderView) modelAliasView {
	view := modelAliasView{
		ID:                 record.ID,
		AliasModelID:       record.AliasModelID,
		TargetModelID:      record.TargetModelID,
		TargetProviderName: record.TargetProviderName,
		Providers:          append([]routeProviderView(nil), providers...),
		Routable:           len(providers) > 0,
		CreatedAt:          record.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:          record.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if record.TargetProviderID.Valid {
		view.TargetProviderID = record.TargetProviderID.Int64
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

// summarizeRequestLogs reduces a slice of audited request records into the
// top-line counts shown in the stats dashboard.
func summarizeRequestLogs(logs []proxyRequestLogRecord) requestStatsSummaryView {
	summary := requestStatsSummaryView{}
	for _, log := range logs {
		summary.Requests++
		applyRequestLogOutcome(&summary.Succeeded, &summary.Failed, log)
		applyRequestLogUsage(
			&summary.ConsumedTokens,
			&summary.CachedInputTokens,
			&summary.NonCachedInputTokens,
			&summary.OutputTokens,
			log,
		)
	}

	return summary
}

// buildDailyRequestStatsBuckets aggregates logs into 7 calendar-day buckets
// starting at the provided UTC midnight.
func buildDailyRequestStatsBuckets(logs []proxyRequestLogRecord, start time.Time) []requestStatsBucketView {
	frames := make([]requestStatsBucketFrame, 0, 7)
	for index := 0; index < 7; index++ {
		bucketStart := start.AddDate(0, 0, index)
		frames = append(frames, requestStatsBucketFrame{
			Start: bucketStart,
			End:   bucketStart.Add(24 * time.Hour),
			Label: bucketStart.Format("Jan 2"),
		})
	}

	applyRequestLogsToBuckets(frames, logs)
	return requestStatsBucketFramesToViews(frames)
}

// buildHourlyRequestStatsBuckets aggregates logs into 12 hourly buckets
// starting at the provided UTC hour boundary.
func buildHourlyRequestStatsBuckets(logs []proxyRequestLogRecord, start time.Time) []requestStatsBucketView {
	frames := make([]requestStatsBucketFrame, 0, 12)
	for index := 0; index < 12; index++ {
		bucketStart := start.Add(time.Duration(index) * time.Hour)
		frames = append(frames, requestStatsBucketFrame{
			Start: bucketStart,
			End:   bucketStart.Add(time.Hour),
			Label: bucketStart.Format("15:00"),
		})
	}

	applyRequestLogsToBuckets(frames, logs)
	return requestStatsBucketFramesToViews(frames)
}

// applyRequestLogsToBuckets updates the matching bucket for each audited request.
func applyRequestLogsToBuckets(frames []requestStatsBucketFrame, logs []proxyRequestLogRecord) {
	if len(frames) == 0 {
		return
	}

	for _, log := range logs {
		for index := range frames {
			if log.RequestedAt.Before(frames[index].Start) || !log.RequestedAt.Before(frames[index].End) {
				continue
			}

			frames[index].Requests++
			applyRequestLogOutcome(&frames[index].Succeeded, &frames[index].Failed, log)
			applyRequestLogUsage(
				&frames[index].ConsumedTokens,
				&frames[index].CachedInputTokens,
				&frames[index].NonCachedInputTokens,
				&frames[index].OutputTokens,
				log,
			)
			break
		}
	}
}

// requestStatsBucketFramesToViews converts bucket frames into the JSON payload
// returned by the stats API.
func requestStatsBucketFramesToViews(frames []requestStatsBucketFrame) []requestStatsBucketView {
	views := make([]requestStatsBucketView, 0, len(frames))
	for _, frame := range frames {
		views = append(views, requestStatsBucketView{
			Start:                frame.Start.UTC().Format(time.RFC3339),
			Label:                frame.Label,
			Requests:             frame.Requests,
			Succeeded:            frame.Succeeded,
			Failed:               frame.Failed,
			ConsumedTokens:       frame.ConsumedTokens,
			CachedInputTokens:    frame.CachedInputTokens,
			NonCachedInputTokens: frame.NonCachedInputTokens,
			OutputTokens:         frame.OutputTokens,
		})
	}

	return views
}

// applyRequestLogOutcome updates success and failure counters for one audited request.
func applyRequestLogOutcome(succeeded *int64, failed *int64, log proxyRequestLogRecord) {
	if !log.CompletedAt.Valid {
		return
	}

	if log.ResponseStatus.Valid && log.ResponseStatus.Int64 >= 200 && log.ResponseStatus.Int64 < 300 {
		*succeeded = *succeeded + 1
		return
	}

	*failed = *failed + 1
}

// applyRequestLogUsage adds the parsed token usage from one audited response to
// the provided running totals.
func applyRequestLogUsage(consumedTokens *int64, cachedInputTokens *int64, nonCachedInputTokens *int64, outputTokens *int64, log proxyRequestLogRecord) {
	usage, ok := extractRequestTokenUsage(log.ResponseHeaders, log.ResponseBody)
	if !ok {
		return
	}

	*consumedTokens += usage.InputTokens + usage.OutputTokens
	*cachedInputTokens += usage.CachedInputTokens
	*nonCachedInputTokens += usage.NonCachedInputTokens
	*outputTokens += usage.OutputTokens
}

// extractRequestTokenUsage parses one audited response body using the persisted
// response content type to decide whether the payload should be interpreted as
// plain JSON or as an SSE event stream.
func extractRequestTokenUsage(responseHeaders string, body string) (requestTokenUsage, bool) {
	if isEventStreamResponse(responseHeaders) {
		return extractEventStreamRequestTokenUsage(body)
	}

	return extractJSONRequestTokenUsage(body)
}

// extractJSONRequestTokenUsage parses one OpenAI-style JSON response body and
// returns the token counts needed by the stats dashboard.
func extractJSONRequestTokenUsage(body string) (requestTokenUsage, bool) {
	if strings.TrimSpace(body) == "" || !json.Valid([]byte(body)) {
		return requestTokenUsage{}, false
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return requestTokenUsage{}, false
	}

	usage := usageMap(payload)
	if len(usage) == 0 {
		return requestTokenUsage{}, false
	}

	inputTokens := usageMapInt64(usage, "prompt_tokens")
	if inputTokens == 0 {
		inputTokens = usageMapInt64(usage, "input_tokens")
	}

	outputTokens := usageMapInt64(usage, "completion_tokens")
	if outputTokens == 0 {
		outputTokens = usageMapInt64(usage, "output_tokens")
	}

	cachedInputTokens := usageDetailsInt64(usage, "prompt_tokens_details", "cached_tokens")
	if cachedInputTokens == 0 {
		cachedInputTokens = usageDetailsInt64(usage, "input_tokens_details", "cached_tokens")
	}
	if cachedInputTokens > inputTokens {
		cachedInputTokens = inputTokens
	}

	if inputTokens == 0 && outputTokens == 0 {
		return requestTokenUsage{}, false
	}

	return requestTokenUsage{
		InputTokens:          inputTokens,
		CachedInputTokens:    cachedInputTokens,
		NonCachedInputTokens: inputTokens - cachedInputTokens,
		OutputTokens:         outputTokens,
	}, true
}

// extractEventStreamRequestTokenUsage scans one SSE response body and returns
// the last usage-bearing JSON chunk that appeared before the terminal `[DONE]`
// event.
func extractEventStreamRequestTokenUsage(body string) (requestTokenUsage, bool) {
	if strings.TrimSpace(body) == "" {
		return requestTokenUsage{}, false
	}

	normalized := strings.ReplaceAll(body, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	var lastUsage requestTokenUsage
	found := false
	for _, rawEvent := range strings.Split(normalized, "\n\n") {
		data, ok := extractSSEEventData(rawEvent)
		if !ok {
			continue
		}

		if strings.TrimSpace(data) == "[DONE]" {
			continue
		}

		usage, ok := extractJSONRequestTokenUsage(data)
		if !ok {
			continue
		}

		lastUsage = usage
		found = true
	}

	return lastUsage, found
}

// extractSSEEventData joins all `data:` lines from one SSE event block into the
// JSON payload seen by the client.
func extractSSEEventData(event string) (string, bool) {
	if strings.TrimSpace(event) == "" {
		return "", false
	}

	lines := strings.Split(event, "\n")
	dataLines := make([]string, 0, len(lines))
	for _, line := range lines {
		if !strings.HasPrefix(line, "data:") {
			continue
		}

		value := strings.TrimPrefix(line, "data:")
		value = strings.TrimPrefix(value, " ")
		dataLines = append(dataLines, value)
	}

	if len(dataLines) == 0 {
		return "", false
	}

	return strings.Join(dataLines, "\n"), true
}

// isEventStreamResponse reports whether the audited response headers identify
// the body as an SSE stream.
func isEventStreamResponse(responseHeaders string) bool {
	return responseContentType(responseHeaders) == "text/event-stream"
}

// responseContentType extracts the normalized media type from the persisted
// response headers JSON so downstream parsers can choose the correct body
// format.
func responseContentType(responseHeaders string) string {
	if strings.TrimSpace(responseHeaders) == "" {
		return ""
	}

	var headers map[string][]string
	if err := json.Unmarshal([]byte(responseHeaders), &headers); err != nil {
		return ""
	}

	for key, values := range headers {
		if !strings.EqualFold(key, "Content-Type") {
			continue
		}

		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" {
				continue
			}

			mediaType, _, err := mime.ParseMediaType(trimmed)
			if err == nil {
				return strings.ToLower(mediaType)
			}

			return strings.ToLower(strings.TrimSpace(strings.Split(trimmed, ";")[0]))
		}
	}

	return ""
}

// usageMap returns the top-level usage object for plain JSON responses and the
// nested `response.usage` object for websocket terminal events.
func usageMap(payload map[string]any) map[string]any {
	if usage, ok := payload["usage"].(map[string]any); ok {
		return usage
	}

	response, ok := payload["response"].(map[string]any)
	if !ok {
		return nil
	}

	usage, _ := response["usage"].(map[string]any)
	return usage
}

// usageMapInt64 reads an integer field from a generic usage map.
func usageMapInt64(values map[string]any, key string) int64 {
	value, found := values[key]
	if !found {
		return 0
	}

	return anyToInt64(value)
}

// usageDetailsInt64 reads an integer field from a nested usage-details map.
func usageDetailsInt64(values map[string]any, parentKey string, childKey string) int64 {
	parent, found := values[parentKey]
	if !found {
		return 0
	}

	nested, ok := parent.(map[string]any)
	if !ok {
		return 0
	}

	return anyToInt64(nested[childKey])
}

// anyToInt64 converts a generic decoded JSON value into an int64 when possible.
func anyToInt64(value any) int64 {
	switch typed := value.(type) {
	case nil:
		return 0
	case int:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		number, err := typed.Int64()
		if err != nil {
			return 0
		}
		return number
	default:
		return 0
	}
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
