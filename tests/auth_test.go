package server_test

import (
	"net/http"
	"net/http/cookiejar"
	"testing"
)

// testAuthSessionStateResponse mirrors the login and session-status payloads
// returned by the gateway.
type testAuthSessionStateResponse struct {
	// Authenticated reports whether the caller has a valid browser session.
	Authenticated bool `json:"authenticated"`
	// Session contains the current browser session when authentication succeeds.
	Session *testAuthSessionView `json:"session,omitempty"`
}

// testAuthSessionView mirrors one browser session returned by the gateway.
type testAuthSessionView struct {
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

// testAuthSessionsPayload mirrors the session-list payload returned by the
// gateway.
type testAuthSessionsPayload struct {
	// Sessions contains every stored browser session.
	Sessions []testAuthSessionView `json:"sessions"`
}

// testGatewayAPIKeyView mirrors one API key returned by the gateway.
type testGatewayAPIKeyView struct {
	// ID is the API-key identifier.
	ID int64 `json:"id"`
	// Name is the user-facing label shown in the UI.
	Name string `json:"name"`
	// KeyPrefix stores the short preview shown in the UI.
	KeyPrefix string `json:"keyPrefix"`
	// CreatedAt records when the API key was created.
	CreatedAt string `json:"createdAt"`
	// LastUsedAt records the last time the key authenticated a request.
	LastUsedAt string `json:"lastUsedAt,omitempty"`
}

// testGatewayAPIKeysPayload mirrors the API-key list payload returned by the
// gateway.
type testGatewayAPIKeysPayload struct {
	// APIKeys contains every stored API key.
	APIKeys []testGatewayAPIKeyView `json:"apiKeys"`
}

// newCookieClient constructs an HTTP client with a cookie jar so login sessions
// persist across requests.
func newCookieClient(t *testing.T) *http.Client {
	t.Helper()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}

	return &http.Client{Jar: jar}
}

// TestAccessKeySessionAndGatewayAPIKeyAccess verifies that anonymous callers
// are blocked, browser sessions can log in with the shared access key, API keys
// unlock `/v1`, and revoked sessions lose access again.
func TestAccessKeySessionAndGatewayAPIKeyAccess(t *testing.T) {
	t.Parallel()

	gatewayURL, _, _ := newTestGatewayServerWithDatabase(t)

	anonymousClient := &http.Client{}
	doJSONRequest(t, anonymousClient, http.MethodGet, gatewayURL+"/api/providers", "", http.StatusUnauthorized, nil)
	doJSONRequest(t, anonymousClient, http.MethodGet, gatewayURL+"/v1/models", "", http.StatusUnauthorized, nil)

	loginClient := newCookieClient(t)
	var loginPayload testAuthSessionStateResponse
	doJSONRequest(
		t,
		loginClient,
		http.MethodPost,
		gatewayURL+"/api/auth/login",
		`{"accessKey":"`+testAccessKey+`"}`,
		http.StatusOK,
		&loginPayload,
	)
	if !loginPayload.Authenticated || loginPayload.Session == nil || !loginPayload.Session.Current {
		t.Fatalf("login payload = %#v, want authenticated current session", loginPayload)
	}

	var sessionPayload testAuthSessionStateResponse
	doJSONRequest(t, loginClient, http.MethodGet, gatewayURL+"/api/auth/session", "", http.StatusOK, &sessionPayload)
	if !sessionPayload.Authenticated || sessionPayload.Session == nil || !sessionPayload.Session.Current {
		t.Fatalf("session payload = %#v, want authenticated current session", sessionPayload)
	}

	apiKey := createTestGatewayAPIKey(t, loginClient, gatewayURL, "Auth test key")
	v1Client := newAuthenticatedAPIClient(loginClient, apiKey)

	var apiKeysPayload testGatewayAPIKeysPayload
	doJSONRequest(t, loginClient, http.MethodGet, gatewayURL+"/api/auth/api-keys", "", http.StatusOK, &apiKeysPayload)
	if len(apiKeysPayload.APIKeys) != 1 || apiKeysPayload.APIKeys[0].Name != "Auth test key" {
		t.Fatalf("api keys payload = %#v, want created key", apiKeysPayload)
	}
	if apiKeysPayload.APIKeys[0].KeyPrefix == "" {
		t.Fatalf("api keys payload = %#v, want a visible prefix", apiKeysPayload)
	}

	var modelsPayload map[string]any
	doJSONRequest(t, v1Client, http.MethodGet, gatewayURL+"/v1/models", "", http.StatusOK, &modelsPayload)
	if modelsPayload["data"] == nil {
		t.Fatalf("gateway models payload = %#v, want data list", modelsPayload)
	}

	var sessionsPayload testAuthSessionsPayload
	doJSONRequest(t, loginClient, http.MethodGet, gatewayURL+"/api/auth/sessions", "", http.StatusOK, &sessionsPayload)
	if len(sessionsPayload.Sessions) == 0 {
		t.Fatalf("sessions payload = %#v, want at least one session", sessionsPayload)
	}

	currentSessionID := int64(0)
	for _, session := range sessionsPayload.Sessions {
		if session.Current {
			currentSessionID = session.ID
			break
		}
	}
	if currentSessionID == 0 {
		t.Fatalf("sessions payload = %#v, want one current session", sessionsPayload)
	}

	doJSONRequest(
		t,
		loginClient,
		http.MethodDelete,
		gatewayURL+"/api/auth/sessions/"+formatInt64(currentSessionID),
		"",
		http.StatusNoContent,
		nil,
	)

	doJSONRequest(t, loginClient, http.MethodGet, gatewayURL+"/api/auth/session", "", http.StatusOK, &sessionPayload)
	if sessionPayload.Authenticated {
		t.Fatalf("session payload after revoke = %#v, want logged out", sessionPayload)
	}
	doJSONRequest(t, loginClient, http.MethodGet, gatewayURL+"/api/providers", "", http.StatusUnauthorized, nil)
}
