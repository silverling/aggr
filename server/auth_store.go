package server

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// authSessionRecord is the internal database representation of one logged-in
// browser session tracked by the gateway.
type authSessionRecord struct {
	// ID is the session row primary key.
	ID int64
	// TokenHash is the SHA-256 hash of the raw session cookie value.
	TokenHash string
	// UserAgent stores the browser user-agent string captured at login time.
	UserAgent string
	// RemoteAddr stores the remote address captured at login time.
	RemoteAddr string
	// CreatedAt records when the session row was inserted.
	CreatedAt time.Time
	// LastSeenAt records when the session was last successfully used.
	LastSeenAt time.Time
}

// authSessionView is the JSON payload returned to the Web UI for one browser
// session.
type authSessionView struct {
	// ID is the session identifier.
	ID int64 `json:"id"`
	// UserAgent is the browser user-agent string captured at login time.
	UserAgent string `json:"userAgent,omitempty"`
	// RemoteAddr is the remote address captured at login time.
	RemoteAddr string `json:"remoteAddr,omitempty"`
	// CreatedAt records when the session was created.
	CreatedAt string `json:"createdAt"`
	// LastSeenAt records when the session last authenticated successfully.
	LastSeenAt string `json:"lastSeenAt"`
	// Current reports whether the session matches the current request cookie.
	Current bool `json:"current"`
}

// authSessionCreate captures the fields inserted when a browser session is
// created after a successful access-key login.
type authSessionCreate struct {
	// TokenHash is the SHA-256 hash of the raw session cookie value.
	TokenHash string
	// UserAgent stores the browser user-agent string captured at login time.
	UserAgent string
	// RemoteAddr stores the remote address captured at login time.
	RemoteAddr string
	// CreatedAt records when the session row was inserted.
	CreatedAt time.Time
	// LastSeenAt records when the session was first considered active.
	LastSeenAt time.Time
}

// gatewayAPIKeyRecord is the internal database representation of one API key
// that can authenticate requests to the gateway's `/v1` endpoints.
type gatewayAPIKeyRecord struct {
	// ID is the API-key row primary key.
	ID int64
	// Name is the user-facing label shown in the Web UI.
	Name string
	// KeyPrefix stores a short preview of the raw API key.
	KeyPrefix string
	// KeyHash is the SHA-256 hash of the raw API key.
	KeyHash string
	// CreatedAt records when the API key was created.
	CreatedAt time.Time
	// LastUsedAt records the last time the key authenticated a request.
	LastUsedAt sql.NullTime
}

// gatewayAPIKeyView is the JSON payload returned to the Web UI for one API
// key.
type gatewayAPIKeyView struct {
	// ID is the API-key identifier.
	ID int64 `json:"id"`
	// Name is the user-facing label shown in the Web UI.
	Name string `json:"name"`
	// KeyPrefix stores a short preview of the raw API key.
	KeyPrefix string `json:"keyPrefix"`
	// CreatedAt records when the API key was created.
	CreatedAt string `json:"createdAt"`
	// LastUsedAt records the last time the key authenticated a request.
	LastUsedAt string `json:"lastUsedAt,omitempty"`
}

// gatewayAPIKeyCreate captures the fields inserted when the Web UI creates a
// new API key.
type gatewayAPIKeyCreate struct {
	// Name is the user-facing label shown in the Web UI.
	Name string
	// KeyPrefix stores a short preview of the raw API key.
	KeyPrefix string
	// KeyHash is the SHA-256 hash of the raw API key.
	KeyHash string
	// CreatedAt records when the API key row was inserted.
	CreatedAt time.Time
}

// ensureAuthTables creates the session and API-key tables required by the
// authentication layer.
func (s *store) ensureAuthTables(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			token_hash TEXT NOT NULL UNIQUE,
			user_agent TEXT NOT NULL DEFAULT '',
			remote_addr TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions (token_hash);`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_last_seen_at ON sessions (last_seen_at DESC, id DESC);`,
		`CREATE TABLE IF NOT EXISTS gateway_api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			key_prefix TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			last_used_at TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_api_keys_key_hash ON gateway_api_keys (key_hash);`,
		`CREATE INDEX IF NOT EXISTS idx_gateway_api_keys_created_at ON gateway_api_keys (created_at DESC, id DESC);`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply auth migration: %w", err)
		}
	}

	return nil
}

// createSession inserts a new browser session row and returns the persisted
// record.
func (s *store) createSession(ctx context.Context, entry authSessionCreate) (authSessionRecord, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (token_hash, user_agent, remote_addr, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?)
	`, entry.TokenHash, entry.UserAgent, entry.RemoteAddr, entry.CreatedAt.UTC().Format(time.RFC3339), entry.LastSeenAt.UTC().Format(time.RFC3339))
	if err != nil {
		return authSessionRecord{}, fmt.Errorf("insert session: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return authSessionRecord{}, fmt.Errorf("read session id: %w", err)
	}

	return s.getSession(ctx, id)
}

// getSession retrieves one browser session by its database identifier.
func (s *store) getSession(ctx context.Context, id int64) (authSessionRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, token_hash, user_agent, remote_addr, created_at, last_seen_at
		FROM sessions
		WHERE id = ?
	`, id)

	record, err := scanAuthSession(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return authSessionRecord{}, err
		}
		return authSessionRecord{}, err
	}

	return record, nil
}

// getSessionByTokenHash retrieves one browser session by the SHA-256 hash of
// its raw cookie token.
func (s *store) getSessionByTokenHash(ctx context.Context, tokenHash string) (authSessionRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, token_hash, user_agent, remote_addr, created_at, last_seen_at
		FROM sessions
		WHERE token_hash = ?
	`, tokenHash)

	record, err := scanAuthSession(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return authSessionRecord{}, sql.ErrNoRows
		}
		return authSessionRecord{}, err
	}

	return record, nil
}

// listSessions returns every stored browser session ordered from newest to
// oldest.
func (s *store) listSessions(ctx context.Context) ([]authSessionRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, token_hash, user_agent, remote_addr, created_at, last_seen_at
		FROM sessions
		ORDER BY last_seen_at DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]authSessionRecord, 0)
	for rows.Next() {
		record, err := scanAuthSession(rows)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sessions: %w", err)
	}

	return sessions, nil
}

// touchSession updates the last-seen timestamp for one active browser session.
func (s *store) touchSession(ctx context.Context, id int64, seenAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE sessions
		SET last_seen_at = ?
		WHERE id = ?
	`, seenAt.UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("update session last_seen_at: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated session rows: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// deleteSession removes one browser session from the database.
func (s *store) deleteSession(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted session rows: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// createGatewayAPIKey inserts a new API key row and returns the persisted
// record.
func (s *store) createGatewayAPIKey(ctx context.Context, entry gatewayAPIKeyCreate) (gatewayAPIKeyRecord, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO gateway_api_keys (name, key_prefix, key_hash, created_at)
		VALUES (?, ?, ?, ?)
	`, entry.Name, entry.KeyPrefix, entry.KeyHash, entry.CreatedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return gatewayAPIKeyRecord{}, fmt.Errorf("insert gateway API key: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return gatewayAPIKeyRecord{}, fmt.Errorf("read gateway API key id: %w", err)
	}

	return s.getGatewayAPIKey(ctx, id)
}

// getGatewayAPIKey retrieves one API key by its database identifier.
func (s *store) getGatewayAPIKey(ctx context.Context, id int64) (gatewayAPIKeyRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, key_prefix, key_hash, created_at, last_used_at
		FROM gateway_api_keys
		WHERE id = ?
	`, id)

	record, err := scanGatewayAPIKey(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return gatewayAPIKeyRecord{}, sql.ErrNoRows
		}
		return gatewayAPIKeyRecord{}, err
	}

	return record, nil
}

// getGatewayAPIKeyByHash retrieves one API key by the SHA-256 hash of its raw
// bearer token.
func (s *store) getGatewayAPIKeyByHash(ctx context.Context, keyHash string) (gatewayAPIKeyRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, key_prefix, key_hash, created_at, last_used_at
		FROM gateway_api_keys
		WHERE key_hash = ?
	`, keyHash)

	record, err := scanGatewayAPIKey(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return gatewayAPIKeyRecord{}, sql.ErrNoRows
		}
		return gatewayAPIKeyRecord{}, err
	}

	return record, nil
}

// listGatewayAPIKeys returns every stored API key ordered from newest to oldest.
func (s *store) listGatewayAPIKeys(ctx context.Context) ([]gatewayAPIKeyRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, key_prefix, key_hash, created_at, last_used_at
		FROM gateway_api_keys
		ORDER BY created_at DESC, id DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query gateway API keys: %w", err)
	}
	defer rows.Close()

	keys := make([]gatewayAPIKeyRecord, 0)
	for rows.Next() {
		record, err := scanGatewayAPIKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate gateway API keys: %w", err)
	}

	return keys, nil
}

// touchGatewayAPIKey updates the last-used timestamp for one active API key.
func (s *store) touchGatewayAPIKey(ctx context.Context, id int64, usedAt time.Time) error {
	result, err := s.db.ExecContext(ctx, `
		UPDATE gateway_api_keys
		SET last_used_at = ?
		WHERE id = ?
	`, usedAt.UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("update gateway API key last_used_at: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read updated gateway API key rows: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// deleteGatewayAPIKey removes one API key from the database.
func (s *store) deleteGatewayAPIKey(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM gateway_api_keys WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete gateway API key: %w", err)
	}

	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read deleted gateway API key rows: %w", err)
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// scanAuthSession reads a session row from either a query row or rows iterator.
func scanAuthSession(scanner interface {
	Scan(dest ...any) error
}) (authSessionRecord, error) {
	var (
		record       authSessionRecord
		createdAtRaw string
		lastSeenRaw  string
	)

	if err := scanner.Scan(
		&record.ID,
		&record.TokenHash,
		&record.UserAgent,
		&record.RemoteAddr,
		&createdAtRaw,
		&lastSeenRaw,
	); err != nil {
		return authSessionRecord{}, err
	}

	createdAt, err := time.Parse(time.RFC3339, createdAtRaw)
	if err != nil {
		return authSessionRecord{}, fmt.Errorf("parse session created_at: %w", err)
	}
	lastSeenAt, err := time.Parse(time.RFC3339, lastSeenRaw)
	if err != nil {
		return authSessionRecord{}, fmt.Errorf("parse session last_seen_at: %w", err)
	}

	record.CreatedAt = createdAt
	record.LastSeenAt = lastSeenAt

	return record, nil
}

// scanGatewayAPIKey reads an API-key row from either a query row or rows iterator.
func scanGatewayAPIKey(scanner interface {
	Scan(dest ...any) error
}) (gatewayAPIKeyRecord, error) {
	var (
		record        gatewayAPIKeyRecord
		createdAtRaw  string
		lastUsedAtRaw sql.NullString
	)

	if err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.KeyPrefix,
		&record.KeyHash,
		&createdAtRaw,
		&lastUsedAtRaw,
	); err != nil {
		return gatewayAPIKeyRecord{}, err
	}

	createdAt, err := time.Parse(time.RFC3339, createdAtRaw)
	if err != nil {
		return gatewayAPIKeyRecord{}, fmt.Errorf("parse gateway API key created_at: %w", err)
	}
	record.CreatedAt = createdAt

	if lastUsedAtRaw.Valid {
		lastUsedAt, err := time.Parse(time.RFC3339, lastUsedAtRaw.String)
		if err != nil {
			return gatewayAPIKeyRecord{}, fmt.Errorf("parse gateway API key last_used_at: %w", err)
		}
		record.LastUsedAt = sql.NullTime{Time: lastUsedAt, Valid: true}
	}

	return record, nil
}

// toView converts the internal session record into the JSON payload used by
// the Web UI.
func (record authSessionRecord) toView(current bool) authSessionView {
	view := authSessionView{
		ID:         record.ID,
		UserAgent:  record.UserAgent,
		RemoteAddr: record.RemoteAddr,
		CreatedAt:  record.CreatedAt.UTC().Format(time.RFC3339),
		LastSeenAt: record.LastSeenAt.UTC().Format(time.RFC3339),
		Current:    current,
	}

	return view
}

// toView converts the internal API-key record into the JSON payload used by
// the Web UI.
func (record gatewayAPIKeyRecord) toView() gatewayAPIKeyView {
	view := gatewayAPIKeyView{
		ID:        record.ID,
		Name:      record.Name,
		KeyPrefix: record.KeyPrefix,
		CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339),
	}
	if record.LastUsedAt.Valid {
		view.LastUsedAt = record.LastUsedAt.Time.UTC().Format(time.RFC3339)
	}

	return view
}
