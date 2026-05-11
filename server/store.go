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

// providerMutation captures the normalized fields used when creating or updating a provider.
type providerMutation struct {
	// Name is the normalized provider label.
	Name string
	// BaseURL is the normalized upstream base URL.
	BaseURL string
	// APIKey is the trimmed upstream API key.
	APIKey string
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
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply migration: %w", err)
		}
	}

	return nil
}

// listProviders returns all providers ordered by enabled status and name.
func (s *store) listProviders(ctx context.Context) ([]providerRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, base_url, api_key, enabled, last_error, last_synced_at, created_at, updated_at
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
		SELECT id, name, base_url, api_key, enabled, last_error, last_synced_at, created_at, updated_at
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
		INSERT INTO providers (name, base_url, api_key, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, mutation.Name, mutation.BaseURL, mutation.APIKey, boolToInt(mutation.Enabled), now, now)
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
		SET name = ?, base_url = ?, api_key = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, mutation.Name, mutation.BaseURL, apiKey, boolToInt(mutation.Enabled), nowRFC3339(), id)
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
		SELECT p.id, p.name, p.base_url, p.api_key, p.enabled, p.last_error, p.last_synced_at, p.created_at, p.updated_at
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
