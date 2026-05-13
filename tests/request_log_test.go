package server_test

import (
	"bytes"
	"context"
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
	"time"

	"github.com/coder/websocket"
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
	Requests []testProxyRequestLogSummary `json:"requests"`
}

// testProxyRequestLogSummary mirrors the lightweight request-log list payload
// returned by the dashboard audit feed.
type testProxyRequestLogSummary struct {
	// ID is the audit row identifier.
	ID int64 `json:"id"`
	// ProviderID is the selected provider identifier, when the request was proxied.
	ProviderID *int64 `json:"providerId,omitempty"`
	// ProviderName is the provider label recorded by the gateway.
	ProviderName string `json:"providerName,omitempty"`
	// ModelID is the OpenAI model identifier used for routing.
	ModelID string `json:"modelId,omitempty"`
	// Method is the inbound HTTP verb.
	Method string `json:"method"`
	// Path is the inbound request path.
	Path string `json:"path"`
	// RawQuery is the inbound request query string.
	RawQuery string `json:"rawQuery,omitempty"`
	// Status is the final HTTP status returned to the caller.
	Status int `json:"status,omitempty"`
	// Error stores the final error message when the request fails.
	Error string `json:"error,omitempty"`
	// DurationMS stores the elapsed time in milliseconds.
	DurationMS int64 `json:"durationMs,omitempty"`
	// CachedInputTokens stores the input tokens served from cache.
	CachedInputTokens int64 `json:"cachedInputTokens"`
	// NonCachedInputTokens stores the input tokens that were not cached.
	NonCachedInputTokens int64 `json:"nonCachedInputTokens"`
	// OutputTokens stores the generated output tokens.
	OutputTokens int64 `json:"outputTokens"`
	// TotalTokens stores the sum of input and output tokens.
	TotalTokens int64 `json:"totalTokens"`
	// RequestedAt records when the request arrived at the gateway.
	RequestedAt string `json:"requestedAt"`
	// CompletedAt records when the request finished, when available.
	CompletedAt string `json:"completedAt,omitempty"`
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

// testProxyRequestLog mirrors the detailed request-log payload returned by the
// single-record inspector endpoint.
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
	// DurationMS stores the elapsed time in milliseconds.
	DurationMS int64 `json:"durationMs,omitempty"`
	// CachedInputTokens stores the input tokens served from cache.
	CachedInputTokens int64 `json:"cachedInputTokens"`
	// NonCachedInputTokens stores the input tokens that were not cached.
	NonCachedInputTokens int64 `json:"nonCachedInputTokens"`
	// OutputTokens stores the generated output tokens.
	OutputTokens int64 `json:"outputTokens"`
	// TotalTokens stores the sum of input and output tokens.
	TotalTokens int64 `json:"totalTokens"`
	// RequestedAt records when the request arrived at the gateway.
	RequestedAt string `json:"requestedAt"`
	// CompletedAt records when the request finished, when available.
	CompletedAt string `json:"completedAt,omitempty"`
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

// testModelAliasPayload mirrors the subset of the alias API used by the test
// helper that creates alias routes through the admin API.
type testModelAliasPayload struct {
	// AliasModelID is the public alias exposed by the gateway.
	AliasModelID string `json:"aliasModelId"`
	// TargetModelID is the upstream model name selected by the alias.
	TargetModelID string `json:"targetModelId"`
	// TargetProviderID optionally pins the alias to one provider.
	TargetProviderID *int64 `json:"targetProviderId,omitempty"`
}

// websocketDialHeaderRoundTripper adds a bearer token to websocket handshake
// requests so tests can authenticate `/v1/responses` websocket-mode sessions.
type websocketDialHeaderRoundTripper struct {
	// base is the transport used after the bearer token is added.
	base http.RoundTripper
	// apiKey is the raw bearer token added to the handshake request.
	apiKey string
}

// RoundTrip injects the Authorization header into a websocket handshake request.
func (roundTripper *websocketDialHeaderRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
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
	if chatLog.Path != "/v1/chat/completions" {
		t.Fatalf("chat request path = %q, want %q", chatLog.Path, "/v1/chat/completions")
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
	if chatLog.Method != http.MethodPost {
		t.Fatalf("chat request method = %q, want %q", chatLog.Method, http.MethodPost)
	}
	if chatLog.Status != http.StatusOK {
		t.Fatalf("chat response status = %d, want %d", chatLog.Status, http.StatusOK)
	}
	if chatLog.CachedInputTokens != 0 || chatLog.NonCachedInputTokens != 0 || chatLog.OutputTokens != 0 || chatLog.TotalTokens != 0 {
		t.Fatalf("chat summary tokens = %#v, want zero usage because upstream response omitted usage", chatLog)
	}

	modelsLog := logsPayload.Requests[1]
	if modelsLog.Path != "/v1/models" {
		t.Fatalf("models request path = %q, want %q", modelsLog.Path, "/v1/models")
	}
	if modelsLog.ProviderID != nil {
		t.Fatalf("models request provider id = %v, want nil", modelsLog.ProviderID)
	}
	if modelsLog.Status != http.StatusOK {
		t.Fatalf("models response status = %d, want %d", modelsLog.Status, http.StatusOK)
	}

	chatLogDetail := loadTestProxyRequestLog(t, client, gatewayURL, chatLog.ID)
	if chatLogDetail.SentRequest == nil {
		t.Fatalf("chat sent request = nil, want populated request snapshot")
	}
	if chatLogDetail.SentRequest.Method != http.MethodPost {
		t.Fatalf("chat sent request method = %q, want %q", chatLogDetail.SentRequest.Method, http.MethodPost)
	}
	if !strings.Contains(chatLogDetail.SentRequest.URL, "/v1/chat/completions") {
		t.Fatalf("chat sent request url = %q, expected upstream chat path", chatLogDetail.SentRequest.URL)
	}
	if !strings.Contains(chatLogDetail.SentRequest.Headers, userAgent) {
		t.Fatalf("chat sent request headers = %q, expected upstream user agent", chatLogDetail.SentRequest.Headers)
	}
	if !strings.Contains(chatLogDetail.SentRequest.Body, `"model":"gpt-4.1"`) {
		t.Fatalf("chat sent request body = %q, expected model hint", chatLogDetail.SentRequest.Body)
	}
	if !strings.Contains(chatLogDetail.ReceivedRequest.Body, `"model":"gpt-4.1"`) {
		t.Fatalf("chat request body = %q, expected model hint", chatLogDetail.ReceivedRequest.Body)
	}
	if !strings.Contains(chatLogDetail.ReceivedResponse.Headers, "X-Aggr-Provider") {
		t.Fatalf("chat response headers = %q, expected X-Aggr-Provider", chatLogDetail.ReceivedResponse.Headers)
	}
	if !strings.Contains(chatLogDetail.ReceivedResponse.Body, `"object":"chat.completion"`) {
		t.Fatalf("chat response body = %q, expected completion payload", chatLogDetail.ReceivedResponse.Body)
	}

	modelsLogDetail := loadTestProxyRequestLog(t, client, gatewayURL, modelsLog.ID)
	if modelsLogDetail.SentRequest != nil {
		t.Fatalf("models sent request = %#v, want nil", modelsLogDetail.SentRequest)
	}
	if !strings.Contains(modelsLogDetail.ReceivedResponse.Body, `"object":"list"`) {
		t.Fatalf("models response body = %q, expected aggregated models payload", modelsLogDetail.ReceivedResponse.Body)
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
	if logsPayload.Requests[0].Path != "/v1/models" {
		t.Fatalf("remaining request path = %q, want %q", logsPayload.Requests[0].Path, "/v1/models")
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

// TestRequestLogListIncludesUnfinishedRequests verifies that the admin request
// log API can list audit rows whose response metadata is still NULL because the
// proxied request has not finished yet.
func TestRequestLogListIncludesUnfinishedRequests(t *testing.T) {
	t.Parallel()

	gatewayURL, client, db := newTestGatewayServerWithDatabase(t)
	requestedAt := time.Now().UTC().Add(-time.Minute)

	if _, err := db.Exec(`
		INSERT INTO proxy_request_logs (
			provider_id, provider_name, model_id, method, path, raw_query,
			request_headers, request_body, request_body_truncated, requested_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		nil,
		"",
		"gpt-4.1",
		http.MethodPost,
		"/v1/chat/completions",
		"",
		`{"content-type":"application/json"}`,
		`{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`,
		0,
		requestedAt.Format(time.RFC3339),
	); err != nil {
		t.Fatalf("insert unfinished proxy request log: %v", err)
	}

	var logsPayload testProxyRequestsResponse
	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/requests?limit=10", "", http.StatusOK, &logsPayload)

	if len(logsPayload.Requests) != 1 {
		t.Fatalf("expected 1 unfinished request log, got %d", len(logsPayload.Requests))
	}

	log := logsPayload.Requests[0]
	if log.Path != "/v1/chat/completions" {
		t.Fatalf("unfinished request path = %q, want %q", log.Path, "/v1/chat/completions")
	}
	if log.ModelID != "gpt-4.1" {
		t.Fatalf("unfinished request model = %q, want %q", log.ModelID, "gpt-4.1")
	}
	if log.Status != 0 {
		t.Fatalf("unfinished response status = %d, want 0", log.Status)
	}
	if log.Error != "" {
		t.Fatalf("unfinished response error = %q, want empty string", log.Error)
	}
	if log.CachedInputTokens != 0 || log.NonCachedInputTokens != 0 || log.OutputTokens != 0 || log.TotalTokens != 0 {
		t.Fatalf("unfinished summary tokens = %#v, want zero token counts", log)
	}

	logDetail := loadTestProxyRequestLog(t, client, gatewayURL, log.ID)
	if logDetail.SentRequest != nil {
		t.Fatalf("unfinished sent request = %#v, want nil", logDetail.SentRequest)
	}
	if logDetail.ReceivedResponse.Headers != "" {
		t.Fatalf("unfinished response headers = %q, want empty string", logDetail.ReceivedResponse.Headers)
	}
	if logDetail.ReceivedResponse.Body != "" {
		t.Fatalf("unfinished response body = %q, want empty string", logDetail.ReceivedResponse.Body)
	}
	if logDetail.ReceivedResponse.Error != "" {
		t.Fatalf("unfinished response error = %q, want empty string", logDetail.ReceivedResponse.Error)
	}
	if logDetail.ReceivedResponse.BodyTruncated {
		t.Fatalf("unfinished response body truncated = %t, want false", logDetail.ReceivedResponse.BodyTruncated)
	}
}

// TestProxyHTTPUserAgentPreservation verifies that proxied HTTP requests keep
// the caller's User-Agent when the provider does not override it, fall back to
// the provider value when the caller omits it, and let an explicit provider
// override win when configured.
func TestProxyHTTPUserAgentPreservation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		providerUserAgent string
		requestUserAgent  string
		wantUserAgent     string
	}{
		{
			name:              "preserves caller header when provider has no override",
			providerUserAgent: "",
			requestUserAgent:  "CallerHTTP/1.0",
			wantUserAgent:     "CallerHTTP/1.0",
		},
		{
			name:              "uses provider header when caller omits one",
			providerUserAgent: "ProviderHTTP/1.0",
			requestUserAgent:  "",
			wantUserAgent:     "ProviderHTTP/1.0",
		},
		{
			name:              "provider override wins over caller header",
			providerUserAgent: "ProviderHTTP/2.0",
			requestUserAgent:  "CallerHTTP/2.0",
			wantUserAgent:     "ProviderHTTP/2.0",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var upstreamMu sync.Mutex
			var upstreamUserAgent string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/models" {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4.1"}]}`)
					return
				}

				if r.URL.Path != "/v1/chat/completions" {
					http.NotFound(w, r)
					return
				}

				upstreamMu.Lock()
				upstreamUserAgent = r.Header.Get("User-Agent")
				upstreamMu.Unlock()

				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				_, _ = io.WriteString(w, `{"id":"chatcmpl_test","object":"chat.completion","model":"gpt-4.1"}`)
			}))
			defer upstream.Close()

			gatewayURL, client := newTestGatewayServer(t)
			createTestProvider(t, client, gatewayURL, upstream.URL+"/v1", testCase.providerUserAgent)
			apiKey := createTestGatewayAPIKey(t, client, gatewayURL, "User-Agent HTTP test key")
			v1Client := newAuthenticatedAPIClient(client, apiKey)

			request, err := http.NewRequest(http.MethodPost, gatewayURL+"/v1/chat/completions", strings.NewReader(`{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`))
			if err != nil {
				t.Fatalf("create proxy request: %v", err)
			}
			request.Header.Set("Content-Type", "application/json")
			if testCase.requestUserAgent == "" {
				request.Header.Set("User-Agent", "")
			} else {
				request.Header.Set("User-Agent", testCase.requestUserAgent)
			}

			response, err := v1Client.Do(request)
			if err != nil {
				t.Fatalf("perform proxy request: %v", err)
			}
			defer response.Body.Close()

			if response.StatusCode != http.StatusOK {
				body, readErr := io.ReadAll(response.Body)
				if readErr != nil {
					t.Fatalf("read failing response body: %v", readErr)
				}
				t.Fatalf("proxy status = %d, want %d; body = %s", response.StatusCode, http.StatusOK, string(body))
			}

			upstreamMu.Lock()
			gotUserAgent := upstreamUserAgent
			upstreamMu.Unlock()
			if gotUserAgent != testCase.wantUserAgent {
				t.Fatalf("upstream user agent = %q, want %q", gotUserAgent, testCase.wantUserAgent)
			}
		})
	}
}

// TestProxyHTTPUpstreamHeaderOmissions verifies that proxied HTTP requests do
// not leak forwarding metadata or SDK transport headers to upstream providers.
func TestProxyHTTPUpstreamHeaderOmissions(t *testing.T) {
	t.Parallel()

	var upstreamMu sync.Mutex
	var upstreamHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4.1"}]}`)
			return
		}

		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}

		upstreamMu.Lock()
		upstreamHeaders = r.Header.Clone()
		upstreamMu.Unlock()

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = io.WriteString(w, `{"id":"chatcmpl_test","object":"chat.completion","model":"gpt-4.1"}`)
	}))
	defer upstream.Close()

	gatewayURL, client := newTestGatewayServer(t)
	createTestProvider(t, client, gatewayURL, upstream.URL+"/v1", "")
	apiKey := createTestGatewayAPIKey(t, client, gatewayURL, "Header omission HTTP test key")
	v1Client := newAuthenticatedAPIClient(client, apiKey)

	request, err := http.NewRequest(http.MethodPost, gatewayURL+"/v1/chat/completions?trace=1", strings.NewReader(`{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("create proxy request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "CallerHTTP/omit")
	request.Header.Set("Via", "1.1 Caddy")
	request.Header.Set("X-Forwarded-For", "192.168.1.6")
	request.Header.Set("X-Forwarded-Host", "example.com")
	request.Header.Set("X-Forwarded-Proto", "https")

	response, err := v1Client.Do(request)
	if err != nil {
		t.Fatalf("perform proxy request: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			t.Fatalf("read failing response body: %v", readErr)
		}
		t.Fatalf("proxy status = %d, want %d; body = %s", response.StatusCode, http.StatusOK, string(body))
	}

	upstreamMu.Lock()
	gotHeaders := upstreamHeaders.Clone()
	upstreamMu.Unlock()

	omittedHeaders := []string{
		"Via",
		"X-Forwarded-For",
		"X-Forwarded-Host",
		"X-Forwarded-Proto",
		"X-Stainless-Arch",
		"X-Stainless-Lang",
		"X-Stainless-OS",
		"X-Stainless-Package-Version",
		"X-Stainless-Retry-Count",
		"X-Stainless-Runtime",
		"X-Stainless-Runtime-Version",
	}
	for _, header := range omittedHeaders {
		if gotHeaders.Get(header) != "" {
			t.Fatalf("upstream header %s = %q, want omitted", header, gotHeaders.Get(header))
		}
	}

	if gotHeaders.Get("User-Agent") != "CallerHTTP/omit" {
		t.Fatalf("upstream user agent = %q, want %q", gotHeaders.Get("User-Agent"), "CallerHTTP/omit")
	}
	if gotHeaders.Get("Authorization") != "Bearer test-key" {
		t.Fatalf("upstream authorization = %q, want %q", gotHeaders.Get("Authorization"), "Bearer test-key")
	}
}

// TestHTTPRequestLoggingIncludesStatusCode verifies that the request logging
// middleware records the final HTTP status code alongside the method and path.
func TestHTTPRequestLoggingIncludesStatusCode(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "aggr-logging-test.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	logBuffer := &bytes.Buffer{}
	logger := slog.New(slog.NewTextHandler(logBuffer, &slog.HandlerOptions{}))

	handler, err := server.NewHandler(server.Config{AccessKey: testAccessKey}, db, logger)
	if err != nil {
		t.Fatalf("create gateway handler: %v", err)
	}

	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)

	response, err := http.Get(httpServer.URL + "/healthz")
	if err != nil {
		t.Fatalf("perform health request: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want %d", response.StatusCode, http.StatusOK)
	}

	loggedOutput := logBuffer.String()
	if !strings.Contains(loggedOutput, "msg=\"http request\"") {
		t.Fatalf("logged output = %q, want request log line", loggedOutput)
	}
	if !strings.Contains(loggedOutput, "method=GET") {
		t.Fatalf("logged output = %q, want method field", loggedOutput)
	}
	if !strings.Contains(loggedOutput, "path=/healthz") {
		t.Fatalf("logged output = %q, want path field", loggedOutput)
	}
	if !strings.Contains(loggedOutput, "status=200") {
		t.Fatalf("logged output = %q, want status=200", loggedOutput)
	}
}

// TestResponsesWebSocketMode verifies that `/v1/responses` websocket-mode
// requests route to the correct provider, rewrite alias models before proxying,
// and record the completed turn in the request audit log.
func TestResponsesWebSocketMode(t *testing.T) {
	t.Parallel()

	var upstreamMu sync.Mutex
	var upstreamPayloads []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4.1"}]}`)
			return
		}

		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("accept upstream websocket: %v", err)
			return
		}
		defer conn.CloseNow()

		_, payload, err := conn.Read(context.Background())
		if err != nil {
			t.Errorf("read upstream websocket payload: %v", err)
			return
		}

		upstreamMu.Lock()
		upstreamPayloads = append(upstreamPayloads, string(payload))
		upstreamMu.Unlock()

		if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"response.completed","sequence_number":1,"response":{"id":"resp_test","status":"completed","usage":{"input_tokens":11,"input_tokens_details":{"cached_tokens":3},"output_tokens":7}}}`)); err != nil {
			t.Errorf("write upstream websocket response: %v", err)
			return
		}

		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer upstream.Close()

	gatewayURL, client := newTestGatewayServer(t)
	createTestProvider(t, client, gatewayURL, upstream.URL+"/v1", "AggrWS/1.0")
	createTestModelAlias(t, client, gatewayURL, "alias-responses", "gpt-4.1", nil)
	apiKey := createTestGatewayAPIKey(t, client, gatewayURL, "Responses websocket key")

	conn, _, err := websocket.Dial(context.Background(), strings.Replace(gatewayURL, "http://", "ws://", 1)+"/v1/responses", &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &websocketDialHeaderRoundTripper{apiKey: apiKey},
		},
	})
	if err != nil {
		t.Fatalf("dial gateway websocket: %v", err)
	}
	defer conn.CloseNow()

	if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"response.create","response":{"model":"alias-responses","input":"hello"}}`)); err != nil {
		t.Fatalf("write gateway websocket request: %v", err)
	}

	_, payload, err := conn.Read(context.Background())
	if err != nil {
		t.Fatalf("read gateway websocket response: %v", err)
	}
	if !strings.Contains(string(payload), `"type":"response.completed"`) {
		t.Fatalf("gateway websocket payload = %q, want completed event", string(payload))
	}

	upstreamMu.Lock()
	defer upstreamMu.Unlock()
	if len(upstreamPayloads) != 1 {
		t.Fatalf("upstream websocket payloads = %#v, want one proxied payload", upstreamPayloads)
	}
	if !strings.Contains(upstreamPayloads[0], `"model":"gpt-4.1"`) {
		t.Fatalf("upstream websocket payload = %q, want rewritten upstream model", upstreamPayloads[0])
	}
	if strings.Contains(upstreamPayloads[0], `"model":"alias-responses"`) {
		t.Fatalf("upstream websocket payload = %q, did not expect alias model name", upstreamPayloads[0])
	}

	var logsPayload testProxyRequestsResponse
	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/requests?limit=10", "", http.StatusOK, &logsPayload)
	if len(logsPayload.Requests) == 0 {
		t.Fatalf("request logs = %#v, want at least one log entry", logsPayload)
	}

	log := logsPayload.Requests[0]
	if log.Path != "/v1/responses" {
		t.Fatalf("responses websocket request path = %q, want %q", log.Path, "/v1/responses")
	}
	if log.ModelID != "alias-responses" {
		t.Fatalf("responses websocket model = %q, want %q", log.ModelID, "alias-responses")
	}
	if log.Status != http.StatusOK {
		t.Fatalf("responses websocket response status = %d, want %d", log.Status, http.StatusOK)
	}
	if log.CachedInputTokens != 3 {
		t.Fatalf("responses websocket cached input tokens = %d, want 3", log.CachedInputTokens)
	}
	if log.NonCachedInputTokens != 8 {
		t.Fatalf("responses websocket non-cached input tokens = %d, want 8", log.NonCachedInputTokens)
	}
	if log.OutputTokens != 7 {
		t.Fatalf("responses websocket output tokens = %d, want 7", log.OutputTokens)
	}
	if log.TotalTokens != 18 {
		t.Fatalf("responses websocket total tokens = %d, want 18", log.TotalTokens)
	}

	logDetail := loadTestProxyRequestLog(t, client, gatewayURL, log.ID)
	if logDetail.SentRequest == nil {
		t.Fatalf("responses websocket sent request = nil, want populated request snapshot")
	}
	if !strings.Contains(logDetail.SentRequest.Body, `"model":"gpt-4.1"`) {
		t.Fatalf("responses websocket sent body = %q, want rewritten upstream model", logDetail.SentRequest.Body)
	}
	if !strings.Contains(logDetail.ReceivedResponse.Body, `"type":"response.completed"`) {
		t.Fatalf("responses websocket response body = %q, want completed event", logDetail.ReceivedResponse.Body)
	}
}

// TestResponsesWebSocketUserAgentPreservation verifies that websocket-mode
// upstream dials follow the same User-Agent precedence as proxied HTTP calls.
func TestResponsesWebSocketUserAgentPreservation(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name              string
		providerUserAgent string
		requestUserAgent  string
		wantUserAgent     string
	}{
		{
			name:              "preserves caller header when provider has no override",
			providerUserAgent: "",
			requestUserAgent:  "CallerWS/1.0",
			wantUserAgent:     "CallerWS/1.0",
		},
		{
			name:              "uses provider header when caller omits one",
			providerUserAgent: "ProviderWS/1.0",
			requestUserAgent:  "",
			wantUserAgent:     "ProviderWS/1.0",
		},
		{
			name:              "provider override wins over caller header",
			providerUserAgent: "ProviderWS/2.0",
			requestUserAgent:  "CallerWS/2.0",
			wantUserAgent:     "ProviderWS/2.0",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var upstreamMu sync.Mutex
			var upstreamUserAgent string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v1/models" {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4.1"}]}`)
					return
				}

				if r.URL.Path != "/v1/responses" {
					http.NotFound(w, r)
					return
				}

				upstreamMu.Lock()
				upstreamUserAgent = r.Header.Get("User-Agent")
				upstreamMu.Unlock()

				conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
					InsecureSkipVerify: true,
				})
				if err != nil {
					t.Errorf("accept upstream websocket: %v", err)
					return
				}
				defer conn.CloseNow()

				if _, _, err := conn.Read(context.Background()); err != nil {
					t.Errorf("read upstream websocket payload: %v", err)
					return
				}

				if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"response.completed","sequence_number":1,"response":{"id":"resp_test","status":"completed"}}`)); err != nil {
					t.Errorf("write upstream websocket response: %v", err)
					return
				}

				_ = conn.Close(websocket.StatusNormalClosure, "")
			}))
			defer upstream.Close()

			gatewayURL, client := newTestGatewayServer(t)
			createTestProvider(t, client, gatewayURL, upstream.URL+"/v1", testCase.providerUserAgent)
			createTestModelAlias(t, client, gatewayURL, "alias-responses", "gpt-4.1", nil)
			apiKey := createTestGatewayAPIKey(t, client, gatewayURL, "Responses websocket UA key")

			dialHeaders := http.Header{}
			if testCase.requestUserAgent == "" {
				dialHeaders.Set("User-Agent", "")
			} else {
				dialHeaders.Set("User-Agent", testCase.requestUserAgent)
			}

			conn, _, err := websocket.Dial(context.Background(), strings.Replace(gatewayURL, "http://", "ws://", 1)+"/v1/responses", &websocket.DialOptions{
				HTTPClient: &http.Client{
					Transport: &websocketDialHeaderRoundTripper{apiKey: apiKey},
				},
				HTTPHeader: dialHeaders,
			})
			if err != nil {
				t.Fatalf("dial gateway websocket: %v", err)
			}
			defer conn.CloseNow()

			if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"response.create","response":{"model":"alias-responses","input":"hello"}}`)); err != nil {
				t.Fatalf("write gateway websocket request: %v", err)
			}

			if _, _, err := conn.Read(context.Background()); err != nil {
				t.Fatalf("read gateway websocket response: %v", err)
			}

			upstreamMu.Lock()
			gotUserAgent := upstreamUserAgent
			upstreamMu.Unlock()
			if gotUserAgent != testCase.wantUserAgent {
				t.Fatalf("upstream websocket user agent = %q, want %q", gotUserAgent, testCase.wantUserAgent)
			}
		})
	}
}

// TestResponsesWebSocketUpstreamHeaderOmissions verifies that websocket-mode
// upstream handshakes do not leak forwarding metadata to the provider.
func TestResponsesWebSocketUpstreamHeaderOmissions(t *testing.T) {
	t.Parallel()

	var upstreamMu sync.Mutex
	var upstreamHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4.1"}]}`)
			return
		}

		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}

		upstreamMu.Lock()
		upstreamHeaders = r.Header.Clone()
		upstreamMu.Unlock()

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("accept upstream websocket: %v", err)
			return
		}
		defer conn.CloseNow()

		if _, _, err := conn.Read(context.Background()); err != nil {
			t.Errorf("read upstream websocket payload: %v", err)
			return
		}

		if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"response.completed","sequence_number":1,"response":{"id":"resp_test","status":"completed"}}`)); err != nil {
			t.Errorf("write upstream websocket response: %v", err)
			return
		}

		_ = conn.Close(websocket.StatusNormalClosure, "")
	}))
	defer upstream.Close()

	gatewayURL, client := newTestGatewayServer(t)
	createTestProvider(t, client, gatewayURL, upstream.URL+"/v1", "")
	createTestModelAlias(t, client, gatewayURL, "alias-responses", "gpt-4.1", nil)
	apiKey := createTestGatewayAPIKey(t, client, gatewayURL, "Responses websocket omission key")

	dialHeaders := http.Header{}
	dialHeaders.Set("User-Agent", "CallerWS/omit")
	dialHeaders.Set("Via", "1.1 Caddy")
	dialHeaders.Set("X-Forwarded-For", "192.168.1.6")
	dialHeaders.Set("X-Forwarded-Host", "example.com")
	dialHeaders.Set("X-Forwarded-Proto", "https")

	conn, _, err := websocket.Dial(context.Background(), strings.Replace(gatewayURL, "http://", "ws://", 1)+"/v1/responses", &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &websocketDialHeaderRoundTripper{apiKey: apiKey},
		},
		HTTPHeader: dialHeaders,
	})
	if err != nil {
		t.Fatalf("dial gateway websocket: %v", err)
	}
	defer conn.CloseNow()

	if err := conn.Write(context.Background(), websocket.MessageText, []byte(`{"type":"response.create","response":{"model":"alias-responses","input":"hello"}}`)); err != nil {
		t.Fatalf("write gateway websocket request: %v", err)
	}

	if _, _, err := conn.Read(context.Background()); err != nil {
		t.Fatalf("read gateway websocket response: %v", err)
	}

	upstreamMu.Lock()
	gotHeaders := upstreamHeaders.Clone()
	upstreamMu.Unlock()

	omittedHeaders := []string{
		"Via",
		"X-Forwarded-For",
		"X-Forwarded-Host",
		"X-Forwarded-Proto",
	}
	for _, header := range omittedHeaders {
		if gotHeaders.Get(header) != "" {
			t.Fatalf("upstream websocket header %s = %q, want omitted", header, gotHeaders.Get(header))
		}
	}

	if gotHeaders.Get("User-Agent") != "CallerWS/omit" {
		t.Fatalf("upstream websocket user agent = %q, want %q", gotHeaders.Get("User-Agent"), "CallerWS/omit")
	}
	if gotHeaders.Get("Authorization") != "Bearer test-key" {
		t.Fatalf("upstream websocket authorization = %q, want %q", gotHeaders.Get("Authorization"), "Bearer test-key")
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

// createTestModelAlias inserts one alias through the admin API so websocket
// and HTTP routing tests can expose a stable public model name.
func createTestModelAlias(t *testing.T, client *http.Client, gatewayURL string, aliasModelID string, targetModelID string, targetProviderID *int64) {
	t.Helper()

	body, err := json.Marshal(testModelAliasPayload{
		AliasModelID:     aliasModelID,
		TargetModelID:    targetModelID,
		TargetProviderID: targetProviderID,
	})
	if err != nil {
		t.Fatalf("encode model alias payload: %v", err)
	}

	doJSONRequest(
		t,
		client,
		http.MethodPost,
		gatewayURL+"/api/model-aliases",
		string(body),
		http.StatusCreated,
		nil,
	)
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

// loadTestProxyRequestLog fetches one full audit-log record through the detail
// endpoint so tests can assert the stored headers and bodies on demand.
func loadTestProxyRequestLog(t *testing.T, client *http.Client, gatewayURL string, id int64) testProxyRequestLog {
	t.Helper()

	var payload testProxyRequestLog
	doJSONRequest(
		t,
		client,
		http.MethodGet,
		gatewayURL+"/api/requests/"+strconv.FormatInt(id, 10),
		"",
		http.StatusOK,
		&payload,
	)

	return payload
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
