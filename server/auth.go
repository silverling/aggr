package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// authContextKey identifies request-scoped auth values stored on the context.
type authContextKey string

const (
	// sessionCookieName is the cookie that stores the raw browser session token.
	sessionCookieName = "aggr_session"
	// sessionTokenPrefix makes browser session values easy to recognize when
	// debugging locally.
	sessionTokenPrefix = "aggr_sess_"
	// gatewayAPIKeyPrefix makes gateway API keys easy to recognize when
	// debugging locally.
	gatewayAPIKeyPrefix = "aggr_sk_"
	// sessionEntropyBytes controls the amount of random entropy generated for
	// each browser session token.
	sessionEntropyBytes = 24
	// gatewayAPIKeyEntropyBytes controls the amount of random entropy generated
	// for each gateway API key.
	gatewayAPIKeyEntropyBytes = 24
	// sessionContextKey stores the current browser session on authenticated
	// admin requests.
	sessionContextKey authContextKey = "session"
)

// authSessionStateResponse is the JSON payload returned by the session-status
// endpoint and the login endpoint.
type authSessionStateResponse struct {
	// Authenticated reports whether the request currently has a valid session.
	Authenticated bool `json:"authenticated"`
	// Session contains the current browser session when authentication succeeds.
	Session *authSessionView `json:"session,omitempty"`
	// Version is the build-time gateway version shown by the CLI and Web UI.
	Version string `json:"version"`
}

// authLoginPayload is the JSON body accepted by the login endpoint.
type authLoginPayload struct {
	// AccessKey is the shared secret configured in the `.env` file.
	AccessKey string `json:"accessKey"`
}

// authSessionsPayload is the JSON payload returned when listing browser sessions.
type authSessionsPayload struct {
	// Sessions contains every stored browser session.
	Sessions []authSessionView `json:"sessions"`
}

// gatewayAPIKeyPayload is the JSON body accepted by the API-key create
// endpoint.
type gatewayAPIKeyPayload struct {
	// Name is the user-facing label shown in the Web UI.
	Name string `json:"name"`
}

// gatewayAPIKeysPayload is the JSON payload returned when listing API keys.
type gatewayAPIKeysPayload struct {
	// APIKeys contains every stored gateway API key.
	APIKeys []gatewayAPIKeyView `json:"apiKeys"`
}

// gatewayAPIKeyCreateResponse is the JSON payload returned when a new API key
// is created.
type gatewayAPIKeyCreateResponse struct {
	// APIKey contains the raw bearer token that is only shown once.
	APIKey string `json:"apiKey"`
	// Key contains the persisted API-key metadata.
	Key gatewayAPIKeyView `json:"key"`
}

// withAuth enforces browser-session auth for admin endpoints and bearer-token
// auth for `/v1` endpoints while leaving the public login and session-status
// routes open.
func (s *server) withAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			next.ServeHTTP(w, r)
		case r.URL.Path == "/api/auth/login":
			next.ServeHTTP(w, r)
		case r.URL.Path == "/api/auth/session":
			next.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/v1"):
			key, err := s.authorizeGatewayAPIKey(r)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if key == nil {
				writeError(w, http.StatusUnauthorized, "gateway API key is required")
				return
			}
			next.ServeHTTP(w, r)
		case strings.HasPrefix(r.URL.Path, "/api/"):
			session, err := s.authorizeSession(r)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if session == nil {
				writeError(w, http.StatusUnauthorized, "access key session is required")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionContextKey, *session)))
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// handleAuthSession returns the current session state if the caller has a
// valid session cookie and otherwise reports an unauthenticated state.
func (s *server) handleAuthSession(w http.ResponseWriter, r *http.Request) {
	session, err := s.loadSessionFromRequest(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if session == nil {
		writeJSON(w, http.StatusOK, newAuthSessionStateResponse(false, nil))
		return
	}

	now := time.Now().UTC()
	if err := s.store.touchSession(r.Context(), session.ID, now); err == nil {
		session.LastSeenAt = now
	}

	view := session.toView(true)
	writeJSON(w, http.StatusOK, newAuthSessionStateResponse(true, &view))
}

// handleAuthLogin verifies the shared access key, creates a browser session,
// and returns the current session state after setting the cookie.
func (s *server) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	var payload authLoginPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("decode login payload: %v", err))
		return
	}

	if !s.accessKeyMatches(payload.AccessKey) {
		writeError(w, http.StatusUnauthorized, "invalid access key")
		return
	}

	token, err := generateSecretToken(sessionTokenPrefix, sessionEntropyBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	now := time.Now().UTC()
	session, err := s.store.createSession(r.Context(), authSessionCreate{
		TokenHash:  hashSecret(token),
		UserAgent:  strings.TrimSpace(r.UserAgent()),
		RemoteAddr: strings.TrimSpace(r.RemoteAddr),
		CreatedAt:  now,
		LastSeenAt: now,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.setSessionCookie(w, r, token)
	view := session.toView(true)
	writeJSON(w, http.StatusOK, newAuthSessionStateResponse(true, &view))
}

// newAuthSessionStateResponse constructs the public session payload while
// attaching the current build-time version string for the Web UI.
func newAuthSessionStateResponse(authenticated bool, session *authSessionView) authSessionStateResponse {
	return authSessionStateResponse{
		Authenticated: authenticated,
		Session:       session,
		Version:       Version(),
	}
}

// handleAuthLogout deletes the current browser session and clears the session
// cookie.
func (s *server) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	session := sessionFromContext(r.Context())
	if session != nil {
		if err := s.store.deleteSession(r.Context(), session.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// handleListAuthSessions returns every stored browser session together with a
// flag that marks the current session.
func (s *server) handleListAuthSessions(w http.ResponseWriter, r *http.Request) {
	currentSession := sessionFromContext(r.Context())
	sessions, err := s.store.listSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	views := make([]authSessionView, 0, len(sessions))
	for _, session := range sessions {
		views = append(views, session.toView(currentSession != nil && session.ID == currentSession.ID))
	}

	writeJSON(w, http.StatusOK, authSessionsPayload{Sessions: views})
}

// handleDeleteAuthSession revokes one stored browser session and clears the
// current cookie when the active session is deleted.
func (s *server) handleDeleteAuthSession(w http.ResponseWriter, r *http.Request) {
	id, err := parseAuthSessionID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.store.deleteSession(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	currentSession := sessionFromContext(r.Context())
	if currentSession != nil && currentSession.ID == id {
		s.clearSessionCookie(w, r)
	}

	w.WriteHeader(http.StatusNoContent)
}

// handleListGatewayAPIKeys returns every stored gateway API key.
func (s *server) handleListGatewayAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := s.store.listGatewayAPIKeys(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	views := make([]gatewayAPIKeyView, 0, len(keys))
	for _, key := range keys {
		views = append(views, key.toView())
	}

	writeJSON(w, http.StatusOK, gatewayAPIKeysPayload{APIKeys: views})
}

// handleCreateGatewayAPIKey creates a new bearer token for `/v1` endpoints and
// returns the raw token once alongside the persisted metadata.
func (s *server) handleCreateGatewayAPIKey(w http.ResponseWriter, r *http.Request) {
	var payload gatewayAPIKeyPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("decode API key payload: %v", err))
		return
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	rawKey, err := generateSecretToken(gatewayAPIKeyPrefix, gatewayAPIKeyEntropyBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	key, err := s.store.createGatewayAPIKey(r.Context(), gatewayAPIKeyCreate{
		Name:      name,
		KeyPrefix: previewSecret(rawKey),
		KeyHash:   hashSecret(rawKey),
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, gatewayAPIKeyCreateResponse{
		APIKey: rawKey,
		Key:    key.toView(),
	})
}

// handleDeleteGatewayAPIKey revokes one stored bearer token.
func (s *server) handleDeleteGatewayAPIKey(w http.ResponseWriter, r *http.Request) {
	id, err := parseAuthSessionID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.store.deleteGatewayAPIKey(r.Context(), id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "API key not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// authorizeSession loads a browser session from the request cookie and updates
// its last-seen timestamp when the cookie matches a stored record.
func (s *server) authorizeSession(r *http.Request) (*authSessionRecord, error) {
	session, err := s.loadSessionFromRequest(r)
	if err != nil || session == nil {
		return session, err
	}

	now := time.Now().UTC()
	if err := s.store.touchSession(r.Context(), session.ID, now); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	session.LastSeenAt = now

	return session, nil
}

// authorizeGatewayAPIKey loads a bearer token from the request headers and
// updates its last-used timestamp when it matches a stored record.
func (s *server) authorizeGatewayAPIKey(r *http.Request) (*gatewayAPIKeyRecord, error) {
	rawKey, ok := bearerTokenFromRequest(r)
	if !ok {
		return nil, nil
	}

	key, err := s.store.getGatewayAPIKeyByHash(r.Context(), hashSecret(rawKey))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	now := time.Now().UTC()
	if err := s.store.touchGatewayAPIKey(r.Context(), key.ID, now); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	key.LastUsedAt = sql.NullTime{Time: now, Valid: true}

	return &key, nil
}

// loadSessionFromRequest returns the browser session associated with the
// request cookie when one exists.
func (s *server) loadSessionFromRequest(r *http.Request) (*authSessionRecord, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return nil, nil
		}
		return nil, fmt.Errorf("read session cookie: %w", err)
	}

	tokenHash := hashSecret(cookie.Value)
	session, err := s.store.getSessionByTokenHash(r.Context(), tokenHash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}

	return &session, nil
}

// accessKeyMatches reports whether the provided access key matches the shared
// secret configured for the server.
func (s *server) accessKeyMatches(candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	expected := strings.TrimSpace(s.cfg.AccessKey)
	if candidate == "" || expected == "" || len(candidate) != len(expected) {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(candidate), []byte(expected)) == 1
}

// sessionFromContext returns the browser session stored on the current request
// context, when one is available.
func sessionFromContext(ctx context.Context) *authSessionRecord {
	session, _ := ctx.Value(sessionContextKey).(authSessionRecord)
	if session.ID == 0 {
		return nil
	}

	return &session
}

// setSessionCookie stores the raw session token in an HttpOnly cookie.
func (s *server) setSessionCookie(w http.ResponseWriter, r *http.Request, value string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	})
}

// clearSessionCookie expires the session cookie in the browser.
func (s *server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
		MaxAge:   -1,
	})
}

// bearerTokenFromRequest extracts a bearer token from the Authorization header.
func bearerTokenFromRequest(r *http.Request) (string, bool) {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if raw == "" {
		return "", false
	}

	const prefix = "Bearer "
	if len(raw) < len(prefix) || !strings.EqualFold(raw[:len(prefix)], prefix) {
		return "", false
	}

	token := strings.TrimSpace(raw[len(prefix):])
	if token == "" {
		return "", false
	}

	return token, true
}

// generateSecretToken creates a new random, URL-safe token with a human-readable prefix.
func generateSecretToken(prefix string, entropyBytes int) (string, error) {
	entropy := make([]byte, entropyBytes)
	if _, err := rand.Read(entropy); err != nil {
		return "", fmt.Errorf("generate secret token: %w", err)
	}

	return prefix + base64.RawURLEncoding.EncodeToString(entropy), nil
}

// hashSecret returns the SHA-256 hex digest of a secret token.
func hashSecret(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

// previewSecret shortens a secret token for display in the Web UI.
func previewSecret(value string) string {
	if len(value) <= 12 {
		return value
	}

	return value[:12]
}

// parseAuthSessionID converts a path parameter into a positive integer ID used
// for both sessions and API keys.
func parseAuthSessionID(raw string) (int64, error) {
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid id %q", raw)
	}

	return id, nil
}
