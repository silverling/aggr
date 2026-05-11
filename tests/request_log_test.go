package server_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/silverling/aggr/server"
	_ "modernc.org/sqlite"
)

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
}

// testProxyRequestsResponse mirrors the recent request-log list payload.
type testProxyRequestsResponse struct {
	// Requests contains the most recent audited gateway requests.
	Requests []testProxyRequestLog `json:"requests"`
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
	// Method is the inbound HTTP verb.
	Method string `json:"method"`
	// Path is the inbound request path.
	Path string `json:"path"`
	// RequestBody stores the request payload captured by the gateway.
	RequestBody string `json:"requestBody,omitempty"`
	// ResponseStatus is the final status returned to the caller.
	ResponseStatus int `json:"responseStatus,omitempty"`
	// ResponseHeaders stores the serialized response headers captured by the gateway.
	ResponseHeaders string `json:"responseHeaders,omitempty"`
	// ResponseBody stores the response payload captured by the gateway.
	ResponseBody string `json:"responseBody,omitempty"`
	// RequestedAt records when the request arrived at the gateway.
	RequestedAt string `json:"requestedAt"`
}

// testDeleteProxyRequestsResponse mirrors the deletion response returned by the
// request-log clear endpoint.
type testDeleteProxyRequestsResponse struct {
	// Deleted reports how many rows matched the delete filters.
	Deleted int64 `json:"deleted"`
}

// TestGatewayRequestLogsAndDeletion verifies that `/v1` traffic is audited and
// that the admin API can clear logs by provider and by date range.
func TestGatewayRequestLogsAndDeletion(t *testing.T) {
	t.Parallel()

	upstream := newTestUpstreamServer(t)
	defer upstream.Close()

	gatewayURL := newTestGatewayServer(t)
	provider := createTestProvider(t, gatewayURL, upstream.URL+"/v1")

	var modelsPayload map[string]any
	doJSONRequest(t, http.DefaultClient, http.MethodGet, gatewayURL+"/v1/models", "", http.StatusOK, &modelsPayload)

	var completionPayload map[string]any
	doJSONRequest(
		t,
		http.DefaultClient,
		http.MethodPost,
		gatewayURL+"/v1/chat/completions",
		`{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`,
		http.StatusOK,
		&completionPayload,
	)

	var logsPayload testProxyRequestsResponse
	doJSONRequest(t, http.DefaultClient, http.MethodGet, gatewayURL+"/api/requests?limit=10", "", http.StatusOK, &logsPayload)

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
	if chatLog.ResponseStatus != http.StatusOK {
		t.Fatalf("chat response status = %d, want %d", chatLog.ResponseStatus, http.StatusOK)
	}
	if !strings.Contains(chatLog.RequestBody, `"model":"gpt-4.1"`) {
		t.Fatalf("chat request body = %q, expected model hint", chatLog.RequestBody)
	}
	if !strings.Contains(chatLog.ResponseHeaders, "X-Aggr-Provider") {
		t.Fatalf("chat response headers = %q, expected X-Aggr-Provider", chatLog.ResponseHeaders)
	}
	if !strings.Contains(chatLog.ResponseBody, `"object":"chat.completion"`) {
		t.Fatalf("chat response body = %q, expected completion payload", chatLog.ResponseBody)
	}

	modelsLog := logsPayload.Requests[1]
	if modelsLog.Path != "/v1/models" {
		t.Fatalf("models request path = %q, want %q", modelsLog.Path, "/v1/models")
	}
	if modelsLog.ProviderID != nil {
		t.Fatalf("models request provider id = %v, want nil", modelsLog.ProviderID)
	}
	if !strings.Contains(modelsLog.ResponseBody, `"object":"list"`) {
		t.Fatalf("models response body = %q, expected aggregated models payload", modelsLog.ResponseBody)
	}

	var providerDeletePayload testDeleteProxyRequestsResponse
	doJSONRequest(
		t,
		http.DefaultClient,
		http.MethodDelete,
		gatewayURL+"/api/requests?providerId="+strconv.FormatInt(provider.ID, 10),
		"",
		http.StatusOK,
		&providerDeletePayload,
	)
	if providerDeletePayload.Deleted != 1 {
		t.Fatalf("provider delete count = %d, want 1", providerDeletePayload.Deleted)
	}

	doJSONRequest(t, http.DefaultClient, http.MethodGet, gatewayURL+"/api/requests?limit=10", "", http.StatusOK, &logsPayload)
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
		http.DefaultClient,
		http.MethodDelete,
		gatewayURL+"/api/requests?from="+modelsRequestedAt+"&to="+modelsRequestedAt,
		"",
		http.StatusOK,
		&dateDeletePayload,
	)
	if dateDeletePayload.Deleted != 1 {
		t.Fatalf("date delete count = %d, want 1", dateDeletePayload.Deleted)
	}

	doJSONRequest(t, http.DefaultClient, http.MethodGet, gatewayURL+"/api/requests?limit=10", "", http.StatusOK, &logsPayload)
	if len(logsPayload.Requests) != 0 {
		t.Fatalf("expected 0 request logs after date delete, got %d", len(logsPayload.Requests))
	}
}

// newTestUpstreamServer creates a fake OpenAI-compatible provider used by the
// gateway integration test.
func newTestUpstreamServer(t *testing.T) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
}

// newTestGatewayServer starts the aggr HTTP handler against a temporary SQLite
// database and returns the base URL that tests can call.
func newTestGatewayServer(t *testing.T) string {
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

	handler, err := server.NewHandler(server.Config{}, db, slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{})))
	if err != nil {
		t.Fatalf("create gateway handler: %v", err)
	}

	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	return httpServer.URL
}

// createTestProvider inserts a provider through the public admin API so the
// request-log integration test exercises the same flow as the Web UI.
func createTestProvider(t *testing.T, gatewayURL string, baseURL string) testProviderView {
	t.Helper()

	var payload testProviderCreateResponse
	doJSONRequest(
		t,
		http.DefaultClient,
		http.MethodPost,
		gatewayURL+"/api/providers",
		`{"name":"Primary","baseUrl":"`+baseURL+`","apiKey":"test-key","enabled":true}`,
		http.StatusCreated,
		&payload,
	)

	if payload.Provider.ID <= 0 {
		t.Fatalf("provider id = %d, want positive value", payload.Provider.ID)
	}

	return payload.Provider
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
