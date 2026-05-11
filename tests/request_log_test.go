package server_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/silverling/aggr/server"
	_ "modernc.org/sqlite"
)

// testAccessKey is the shared secret used by the integration tests to log in
// to the Web UI and administrative APIs.
const testAccessKey = "test-access-key"

// authorizationRoundTripper injects a bearer API key into every outbound
// request so tests can call the gateway's `/v1` endpoints.
type authorizationRoundTripper struct {
	// base is the transport used after the Authorization header is injected.
	base http.RoundTripper
	// apiKey is the raw bearer token added to outbound requests.
	apiKey string
}

// RoundTrip clones the request, injects the bearer token, and forwards it to
// the wrapped transport.
func (roundTripper *authorizationRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	if clone.Header.Get("Authorization") == "" {
		clone.Header.Set("Authorization", "Bearer "+roundTripper.apiKey)
	}

	base := roundTripper.base
	if base == nil {
		base = http.DefaultTransport
	}

	return base.RoundTrip(clone)
}

// testProviderCreateResponse mirrors the subset of the provider create payload
// that the integration test needs from the admin API.
type testProviderCreateResponse struct {
	// Provider is the saved provider returned by the gateway.
	Provider testProviderView `json:"provider"`
}

// testProviderView mirrors the provider fields inspected by the integration test.
type testProviderView struct {
	// ID is the database identifier assigned by the server.
	ID int64 `json:"id"`
	// Name is the display label returned by the admin API.
	Name string `json:"name,omitempty"`
	// UserAgent is the optional upstream user-agent string stored for the provider.
	UserAgent string `json:"userAgent,omitempty"`
	// Models lists the provider's synced models.
	Models []string `json:"models,omitempty"`
	// DisabledModels lists the synced models currently blocked by disable rules.
	DisabledModels []string `json:"disabledModels,omitempty"`
}

// testProxyRequestsResponse mirrors the recent request-log list payload.
type testProxyRequestsResponse struct {
	// Requests contains the most recent audited gateway requests.
	Requests []testProxyRequestLog `json:"requests"`
}

// testProxyRequestReceivedRequest mirrors the inbound request section in the
// request-log payload.
type testProxyRequestReceivedRequest struct {
	// Method is the inbound HTTP verb.
	Method string `json:"method"`
	// Path is the inbound request path.
	Path string `json:"path"`
	// RawQuery is the inbound request query string.
	RawQuery string `json:"rawQuery,omitempty"`
	// Headers stores the sanitized inbound headers as JSON.
	Headers string `json:"headers"`
	// Body stores the captured inbound body preview.
	Body string `json:"body,omitempty"`
	// BodyTruncated reports whether the stored request body preview was shortened.
	BodyTruncated bool `json:"bodyTruncated"`
}

// testProxyRequestSentRequest mirrors the upstream request section in the
// request-log payload.
type testProxyRequestSentRequest struct {
	// Method is the outbound HTTP verb.
	Method string `json:"method"`
	// URL is the exact upstream URL the gateway called.
	URL string `json:"url"`
	// Headers stores the sanitized outbound headers as JSON.
	Headers string `json:"headers"`
	// Body stores the captured outbound body preview.
	Body string `json:"body,omitempty"`
	// BodyTruncated reports whether the stored request body preview was shortened.
	BodyTruncated bool `json:"bodyTruncated"`
}

// testProxyRequestReceivedResponse mirrors the response section in the
// request-log payload.
type testProxyRequestReceivedResponse struct {
	// Status is the final HTTP status returned to the caller.
	Status int `json:"status,omitempty"`
	// Headers stores the serialized response headers captured by the gateway.
	Headers string `json:"headers,omitempty"`
	// Body stores the response payload captured by the gateway.
	Body string `json:"body,omitempty"`
	// BodyTruncated reports whether the stored response body preview was shortened.
	BodyTruncated bool `json:"bodyTruncated"`
	// Error stores the final error message when the request fails.
	Error string `json:"error,omitempty"`
}

// testProxyRequestLog mirrors the request-log fields asserted by the integration test.
type testProxyRequestLog struct {
	// ID is the audit row identifier.
	ID int64 `json:"id"`
	// ProviderID is the selected provider identifier, when the request was proxied.
	ProviderID *int64 `json:"providerId,omitempty"`
	// ProviderName is the provider label recorded by the gateway.
	ProviderName string `json:"providerName,omitempty"`
	// ModelID is the OpenAI model identifier used for routing.
	ModelID string `json:"modelId,omitempty"`
	// ReceivedRequest stores the inbound request details.
	ReceivedRequest testProxyRequestReceivedRequest `json:"receivedRequest"`
	// SentRequest stores the upstream request details when the gateway proxied the call.
	SentRequest *testProxyRequestSentRequest `json:"sentRequest,omitempty"`
	// ReceivedResponse stores the final response details returned to the caller.
	ReceivedResponse testProxyRequestReceivedResponse `json:"receivedResponse"`
	// RequestedAt records when the request arrived at the gateway.
	RequestedAt string `json:"requestedAt"`
}

// testDeleteProxyRequestsResponse mirrors the deletion response returned by the
// request-log clear endpoint.
type testDeleteProxyRequestsResponse struct {
	// Deleted reports how many rows matched the delete filters.
	Deleted int64 `json:"deleted"`
}

// testGatewayAPIKeyCreateResponse mirrors the create payload returned by the
// gateway API-key admin endpoint.
type testGatewayAPIKeyCreateResponse struct {
	// APIKey contains the raw bearer token that the server only returns once.
	APIKey string `json:"apiKey"`
}

// TestGatewayRequestLogsAndDeletion verifies that `/v1` traffic is audited and
// that the admin API can clear logs by provider and by date range.
func TestGatewayRequestLogsAndDeletion(t *testing.T) {
	t.Parallel()

	var upstreamMu sync.Mutex
	upstreamUserAgents := make(map[string]string)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamMu.Lock()
		upstreamUserAgents[r.URL.Path] = r.Header.Get("User-Agent")
		upstreamMu.Unlock()

		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4.1"}]}`)
		case "/v1/chat/completions":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read upstream request body: %v", err)
				http.Error(w, "upstream body read failed", http.StatusInternalServerError)
				return
			}
			if !strings.Contains(string(body), `"model":"gpt-4.1"`) {
				t.Errorf("upstream request body = %q, expected model hint", string(body))
				http.Error(w, "missing model hint", http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"id":"chatcmpl_test","object":"chat.completion","model":"gpt-4.1"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	gatewayURL, client := newTestGatewayServer(t)
	const userAgent = "AggrTest/1.0"
	provider := createTestProvider(t, client, gatewayURL, upstream.URL+"/v1", userAgent)
	apiKey := createTestGatewayAPIKey(t, client, gatewayURL, "Request log test key")
	v1Client := newAuthenticatedAPIClient(client, apiKey)

	if provider.UserAgent != userAgent {
		t.Fatalf("provider user agent = %q, want %q", provider.UserAgent, userAgent)
	}

	upstreamMu.Lock()
	modelSyncUserAgent := upstreamUserAgents["/v1/models"]
	upstreamMu.Unlock()
	if modelSyncUserAgent != userAgent {
		t.Fatalf("model sync user agent = %q, want %q", modelSyncUserAgent, userAgent)
	}

	var modelsPayload map[string]any
	doJSONRequest(t, v1Client, http.MethodGet, gatewayURL+"/v1/models", "", http.StatusOK, &modelsPayload)

	var completionPayload map[string]any
	doJSONRequest(
		t,
		v1Client,
		http.MethodPost,
		gatewayURL+"/v1/chat/completions",
		`{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`,
		http.StatusOK,
		&completionPayload,
	)

	upstreamMu.Lock()
	completionUserAgent := upstreamUserAgents["/v1/chat/completions"]
	upstreamMu.Unlock()
	if completionUserAgent != userAgent {
		t.Fatalf("completion user agent = %q, want %q", completionUserAgent, userAgent)
	}

	var logsPayload testProxyRequestsResponse
	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/requests?limit=10", "", http.StatusOK, &logsPayload)

	if len(logsPayload.Requests) != 2 {
		t.Fatalf("expected 2 request logs, got %d", len(logsPayload.Requests))
	}

	chatLog := logsPayload.Requests[0]
	if chatLog.ReceivedRequest.Path != "/v1/chat/completions" {
		t.Fatalf("chat request path = %q, want %q", chatLog.ReceivedRequest.Path, "/v1/chat/completions")
	}
	if chatLog.ModelID != "gpt-4.1" {
		t.Fatalf("chat request model = %q, want %q", chatLog.ModelID, "gpt-4.1")
	}
	if chatLog.ProviderID == nil || *chatLog.ProviderID != provider.ID {
		t.Fatalf("chat request provider id = %v, want %d", chatLog.ProviderID, provider.ID)
	}
	if chatLog.ProviderName != "Primary" {
		t.Fatalf("chat request provider name = %q, want %q", chatLog.ProviderName, "Primary")
	}
	if chatLog.SentRequest == nil {
		t.Fatalf("chat sent request = nil, want populated request snapshot")
	}
	if chatLog.SentRequest.Method != http.MethodPost {
		t.Fatalf("chat sent request method = %q, want %q", chatLog.SentRequest.Method, http.MethodPost)
	}
	if !strings.Contains(chatLog.SentRequest.URL, "/v1/chat/completions") {
		t.Fatalf("chat sent request url = %q, expected upstream chat path", chatLog.SentRequest.URL)
	}
	if !strings.Contains(chatLog.SentRequest.Headers, userAgent) {
		t.Fatalf("chat sent request headers = %q, expected upstream user agent", chatLog.SentRequest.Headers)
	}
	if !strings.Contains(chatLog.SentRequest.Body, `"model":"gpt-4.1"`) {
		t.Fatalf("chat sent request body = %q, expected model hint", chatLog.SentRequest.Body)
	}
	if chatLog.ReceivedResponse.Status != http.StatusOK {
		t.Fatalf("chat response status = %d, want %d", chatLog.ReceivedResponse.Status, http.StatusOK)
	}
	if !strings.Contains(chatLog.ReceivedRequest.Body, `"model":"gpt-4.1"`) {
		t.Fatalf("chat request body = %q, expected model hint", chatLog.ReceivedRequest.Body)
	}
	if !strings.Contains(chatLog.ReceivedResponse.Headers, "X-Aggr-Provider") {
		t.Fatalf("chat response headers = %q, expected X-Aggr-Provider", chatLog.ReceivedResponse.Headers)
	}
	if !strings.Contains(chatLog.ReceivedResponse.Body, `"object":"chat.completion"`) {
		t.Fatalf("chat response body = %q, expected completion payload", chatLog.ReceivedResponse.Body)
	}

	modelsLog := logsPayload.Requests[1]
	if modelsLog.ReceivedRequest.Path != "/v1/models" {
		t.Fatalf("models request path = %q, want %q", modelsLog.ReceivedRequest.Path, "/v1/models")
	}
	if modelsLog.ProviderID != nil {
		t.Fatalf("models request provider id = %v, want nil", modelsLog.ProviderID)
	}
	if modelsLog.SentRequest != nil {
		t.Fatalf("models sent request = %#v, want nil", modelsLog.SentRequest)
	}
	if !strings.Contains(modelsLog.ReceivedResponse.Body, `"object":"list"`) {
		t.Fatalf("models response body = %q, expected aggregated models payload", modelsLog.ReceivedResponse.Body)
	}

	var providerDeletePayload testDeleteProxyRequestsResponse
	doJSONRequest(
		t,
		client,
		http.MethodDelete,
		gatewayURL+"/api/requests?providerId="+strconv.FormatInt(provider.ID, 10),
		"",
		http.StatusOK,
		&providerDeletePayload,
	)
	if providerDeletePayload.Deleted != 1 {
		t.Fatalf("provider delete count = %d, want 1", providerDeletePayload.Deleted)
	}

	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/requests?limit=10", "", http.StatusOK, &logsPayload)
	if len(logsPayload.Requests) != 1 {
		t.Fatalf("expected 1 request log after provider delete, got %d", len(logsPayload.Requests))
	}
	if logsPayload.Requests[0].ReceivedRequest.Path != "/v1/models" {
		t.Fatalf("remaining request path = %q, want %q", logsPayload.Requests[0].ReceivedRequest.Path, "/v1/models")
	}

	modelsRequestedAt := logsPayload.Requests[0].RequestedAt
	var dateDeletePayload testDeleteProxyRequestsResponse
	doJSONRequest(
		t,
		client,
		http.MethodDelete,
		gatewayURL+"/api/requests?from="+modelsRequestedAt+"&to="+modelsRequestedAt,
		"",
		http.StatusOK,
		&dateDeletePayload,
	)
	if dateDeletePayload.Deleted != 1 {
		t.Fatalf("date delete count = %d, want 1", dateDeletePayload.Deleted)
	}

	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/requests?limit=10", "", http.StatusOK, &logsPayload)
	if len(logsPayload.Requests) != 0 {
		t.Fatalf("expected 0 request logs after date delete, got %d", len(logsPayload.Requests))
	}
}

// newTestGatewayServer starts the aggr HTTP handler against a temporary SQLite
// database and returns the base URL together with an authenticated admin client.
func newTestGatewayServer(t *testing.T) (string, *http.Client) {
	t.Helper()

	gatewayURL, client, _ := newTestGatewayServerWithDatabase(t)
	return gatewayURL, client
}

// newTestGatewayServerWithDatabase starts the aggr HTTP handler against a
// temporary SQLite database and returns the base URL, an authenticated admin
// client, and the database handle so tests can seed additional rows directly
// when needed.
func newTestGatewayServerWithDatabase(t *testing.T) (string, *http.Client, *sql.DB) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "aggr-test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	db.SetMaxOpenConns(1)

	handler, err := server.NewHandler(server.Config{AccessKey: testAccessKey}, db, slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})))
	if err != nil {
		t.Fatalf("create gateway handler: %v", err)
	}

	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("create cookie jar: %v", err)
	}
	adminClient := &http.Client{Jar: jar}
	loginTestClient(t, adminClient, httpServer.URL)

	return httpServer.URL, adminClient, db
}

// createTestProvider inserts a provider through the public admin API so the
// request-log integration test exercises the same flow as the Web UI.
func createTestProvider(t *testing.T, client *http.Client, gatewayURL string, baseURL string, userAgent string) testProviderView {
	t.Helper()

	return createNamedTestProvider(t, client, gatewayURL, "Primary", baseURL, userAgent)
}

// createNamedTestProvider inserts a named provider through the public admin API
// so integration tests can target multiple distinct upstreams.
func createNamedTestProvider(t *testing.T, client *http.Client, gatewayURL string, name string, baseURL string, userAgent string) testProviderView {
	t.Helper()

	var payload testProviderCreateResponse
	doJSONRequest(
		t,
		client,
		http.MethodPost,
		gatewayURL+"/api/providers",
		`{"name":"`+name+`","baseUrl":"`+baseURL+`","apiKey":"test-key","userAgent":"`+userAgent+`","enabled":true}`,
		http.StatusCreated,
		&payload,
	)

	if payload.Provider.ID <= 0 {
		t.Fatalf("provider id = %d, want positive value", payload.Provider.ID)
	}
	if payload.Provider.UserAgent != userAgent {
		t.Fatalf("provider user agent = %q, want %q", payload.Provider.UserAgent, userAgent)
	}
	if payload.Provider.Name != name {
		t.Fatalf("provider name = %q, want %q", payload.Provider.Name, name)
	}

	return payload.Provider
}

// createTestGatewayAPIKey creates one gateway API key through the admin API
// and returns the raw bearer token that can be used for `/v1` requests.
func createTestGatewayAPIKey(t *testing.T, client *http.Client, gatewayURL string, name string) string {
	t.Helper()

	var payload testGatewayAPIKeyCreateResponse
	doJSONRequest(
		t,
		client,
		http.MethodPost,
		gatewayURL+"/api/auth/api-keys",
		`{"name":"`+name+`"}`,
		http.StatusCreated,
		&payload,
	)

	if payload.APIKey == "" {
		t.Fatalf("created API key is empty")
	}

	return payload.APIKey
}

// loginTestClient authenticates the provided client against the gateway using
// the shared test access key.
func loginTestClient(t *testing.T, client *http.Client, gatewayURL string) {
	t.Helper()

	doJSONRequest(
		t,
		client,
		http.MethodPost,
		gatewayURL+"/api/auth/login",
		`{"accessKey":"`+testAccessKey+`"}`,
		http.StatusOK,
		nil,
	)
}

// newAuthenticatedAPIClient clones the admin client and injects a bearer token
// into every request.
func newAuthenticatedAPIClient(base *http.Client, apiKey string) *http.Client {
	clone := *base
	clone.Transport = &authorizationRoundTripper{
		base:   base.Transport,
		apiKey: apiKey,
	}
	return &clone
}

// doJSONRequest issues one HTTP request, verifies the status code, and decodes
// the JSON response body into the provided destination value.
func doJSONRequest(t *testing.T, client *http.Client, method string, target string, body string, wantStatus int, destination any) {
	t.Helper()

	var reader io.Reader = http.NoBody
	if body != "" {
		reader = strings.NewReader(body)
	}

	request, err := http.NewRequest(method, target, reader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}

	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	defer response.Body.Close()

	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d; body = %s", method, target, response.StatusCode, wantStatus, string(responseBody))
	}

	if destination == nil {
		return
	}
	if err := json.Unmarshal(responseBody, destination); err != nil {
		t.Fatalf("decode response body: %v; body = %s", err, string(responseBody))
	}
}
