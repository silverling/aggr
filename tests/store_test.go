package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTokenUsageAuditUpstreamServer creates a small OpenAI-compatible upstream
// whose chat-completions endpoint returns the provided content type and body so
// integration tests can verify persisted request-log token summaries.
func newTokenUsageAuditUpstreamServer(t *testing.T, responseContentType string, responseBody string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4.1"}]}`)
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", responseContentType)
			_, _ = io.WriteString(w, responseBody)
		default:
			http.NotFound(w, r)
		}
	}))
}

// loadLatestProxyRequestSummary fetches the most recent request-log summary row
// from the admin API so integration tests can assert the stored token counts.
func loadLatestProxyRequestSummary(t *testing.T, client *http.Client, gatewayURL string) testProxyRequestLogSummary {
	t.Helper()

	var payload testProxyRequestsResponse
	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/requests?limit=10", "", http.StatusOK, &payload)
	if len(payload.Requests) == 0 {
		t.Fatal("expected at least one request-log summary")
	}

	return payload.Requests[0]
}

// TestExtractRequestTokenUsageFromJSONResponse verifies through the public
// request-log summary API that plain JSON responses persist the expected token
// counters after proxying.
func TestExtractRequestTokenUsageFromJSONResponse(t *testing.T) {
	t.Parallel()

	upstream := newTokenUsageAuditUpstreamServer(
		t,
		"application/json; charset=utf-8",
		`{"usage":{"prompt_tokens":120,"completion_tokens":30,"prompt_tokens_details":{"cached_tokens":100}}}`,
	)
	defer upstream.Close()

	gatewayURL, client := newTestGatewayServer(t)
	createTestProvider(t, client, gatewayURL, upstream.URL+"/v1", "JSONUsage/1.0")
	apiKey := createTestGatewayAPIKey(t, client, gatewayURL, "json-usage")
	v1Client := newAuthenticatedAPIClient(client, apiKey)

	doJSONRequest(
		t,
		v1Client,
		http.MethodPost,
		gatewayURL+"/v1/chat/completions",
		`{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`,
		http.StatusOK,
		nil,
	)

	summary := loadLatestProxyRequestSummary(t, client, gatewayURL)
	if summary.CachedInputTokens != 100 {
		t.Fatalf("cached input tokens = %d, want 100", summary.CachedInputTokens)
	}
	if summary.NonCachedInputTokens != 20 {
		t.Fatalf("non-cached input tokens = %d, want 20", summary.NonCachedInputTokens)
	}
	if summary.OutputTokens != 30 {
		t.Fatalf("output tokens = %d, want 30", summary.OutputTokens)
	}
	if summary.TotalTokens != 150 {
		t.Fatalf("total tokens = %d, want 150", summary.TotalTokens)
	}
}

// TestExtractRequestTokenUsageFromEventStreamResponse verifies through the
// public request-log summary API that SSE responses persist the last
// usage-bearing chunk before `[DONE]`.
func TestExtractRequestTokenUsageFromEventStreamResponse(t *testing.T) {
	t.Parallel()

	upstream := newTokenUsageAuditUpstreamServer(
		t,
		"text/event-stream; charset=utf-8",
		"data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n"+
			"data: {\"id\":\"chunk-2\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11540,\"completion_tokens\":32,\"total_tokens\":11572,\"prompt_tokens_details\":{\"cached_tokens\":11520},\"prompt_cache_hit_tokens\":11520,\"prompt_cache_miss_tokens\":20}}\n\n"+
			"data: [DONE]\n\n",
	)
	defer upstream.Close()

	gatewayURL, client := newTestGatewayServer(t)
	createTestProvider(t, client, gatewayURL, upstream.URL+"/v1", "SSEUsage/1.0")
	apiKey := createTestGatewayAPIKey(t, client, gatewayURL, "sse-usage")
	v1Client := newAuthenticatedAPIClient(client, apiKey)

	doJSONRequest(
		t,
		v1Client,
		http.MethodPost,
		gatewayURL+"/v1/chat/completions",
		`{"model":"gpt-4.1","stream":true,"messages":[{"role":"user","content":"hello"}]}`,
		http.StatusOK,
		nil,
	)

	summary := loadLatestProxyRequestSummary(t, client, gatewayURL)
	if summary.CachedInputTokens != 11520 {
		t.Fatalf("cached input tokens = %d, want 11520", summary.CachedInputTokens)
	}
	if summary.NonCachedInputTokens != 20 {
		t.Fatalf("non-cached input tokens = %d, want 20", summary.NonCachedInputTokens)
	}
	if summary.OutputTokens != 32 {
		t.Fatalf("output tokens = %d, want 32", summary.OutputTokens)
	}
	if summary.TotalTokens != 11572 {
		t.Fatalf("total tokens = %d, want 11572", summary.TotalTokens)
	}
}

// TestExtractRequestTokenUsageIgnoresEventStreamPayloadWithoutUsage verifies
// through the public request-log summary API that SSE responses without a
// usage-bearing chunk keep the persisted token counters at zero.
func TestExtractRequestTokenUsageIgnoresEventStreamPayloadWithoutUsage(t *testing.T) {
	t.Parallel()

	upstream := newTokenUsageAuditUpstreamServer(
		t,
		"text/event-stream",
		"data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n"+
			"data: [DONE]\n\n",
	)
	defer upstream.Close()

	gatewayURL, client := newTestGatewayServer(t)
	createTestProvider(t, client, gatewayURL, upstream.URL+"/v1", "SSENoUsage/1.0")
	apiKey := createTestGatewayAPIKey(t, client, gatewayURL, "sse-no-usage")
	v1Client := newAuthenticatedAPIClient(client, apiKey)

	doJSONRequest(
		t,
		v1Client,
		http.MethodPost,
		gatewayURL+"/v1/chat/completions",
		`{"model":"gpt-4.1","stream":true,"messages":[{"role":"user","content":"hello"}]}`,
		http.StatusOK,
		nil,
	)

	summary := loadLatestProxyRequestSummary(t, client, gatewayURL)
	if summary.CachedInputTokens != 0 {
		t.Fatalf("cached input tokens = %d, want 0", summary.CachedInputTokens)
	}
	if summary.NonCachedInputTokens != 0 {
		t.Fatalf("non-cached input tokens = %d, want 0", summary.NonCachedInputTokens)
	}
	if summary.OutputTokens != 0 {
		t.Fatalf("output tokens = %d, want 0", summary.OutputTokens)
	}
	if summary.TotalTokens != 0 {
		t.Fatalf("total tokens = %d, want 0", summary.TotalTokens)
	}
}
