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

type providerRecord struct {
	ID           int64
	Name         string
	BaseURL      string
	APIKey       string
	Enabled      bool
	Models       []string
	LastError    string
	LastSyncedAt *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type providerView struct {
	ID               int64    `json:"id"`
	Name             string   `json:"name"`
	BaseURL          string   `json:"baseUrl"`
	Enabled          bool     `json:"enabled"`
	Models           []string `json:"models"`
	LastError        string   `json:"lastError,omitempty"`
	LastSyncedAt     string   `json:"lastSyncedAt,omitempty"`
	APIKeyConfigured bool     `json:"apiKeyConfigured"`
	APIKeyPreview    string   `json:"apiKeyPreview,omitempty"`
}

type routeProviderView struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type routeModelView struct {
	ID        string              `json:"id"`
	Providers []routeProviderView `json:"providers"`
}

type providerMutation struct {
	Name    string
	BaseURL string
	APIKey  string
	Enabled bool
}

type store struct {
	db *sql.DB
}

func newStore(db *sql.DB) *store {
	return &store{db: db}
}

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

func (s *store) getProvider(ctx context.Context, id int64) (providerRecord, error) {
	return s.getProviderWithModels(ctx, id)
}

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

func maskAPIKey(apiKey string) string {
	if apiKey == "" {
		return ""
	}
	if len(apiKey) <= 8 {
		return strings.Repeat("*", len(apiKey))
	}
	return fmt.Sprintf("%s...%s", apiKey[:4], apiKey[len(apiKey)-4:])
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

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
