package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testModelAliasCreateResponse mirrors the create and update payload returned by
// the model-alias admin API.
type testModelAliasCreateResponse struct {
	// Alias is the saved alias record returned by the gateway.
	Alias testModelAliasView `json:"alias"`
}

// testModelAliasesResponse mirrors the list payload returned by the model-alias
// admin API.
type testModelAliasesResponse struct {
	// Aliases contains the configured alias records returned by the gateway.
	Aliases []testModelAliasView `json:"aliases"`
}

// testModelAliasView mirrors the subset of alias fields asserted by the
// integration test.
type testModelAliasView struct {
	// ID is the alias identifier assigned by the gateway.
	ID int64 `json:"id"`
	// AliasModelID is the public model name exposed by the gateway.
	AliasModelID string `json:"aliasModelId"`
	// TargetModelID is the upstream model name that requests should use.
	TargetModelID string `json:"targetModelId"`
	// TargetProviderID optionally pins the alias to one provider.
	TargetProviderID *int64 `json:"targetProviderId,omitempty"`
	// TargetProviderName is the pinned provider label when configured.
	TargetProviderName string `json:"targetProviderName,omitempty"`
	// Providers lists the enabled providers that currently make the alias routable.
	Providers []testModelProviderSummary `json:"providers"`
	// Routable reports whether the alias currently resolves to at least one route.
	Routable bool `json:"routable"`
}

// testOpenAIModelsResponse mirrors the subset of the OpenAI-style models-list
// payload asserted by the integration test.
type testOpenAIModelsResponse struct {
	// Data contains the aggregated model entries returned by the gateway.
	Data []testOpenAIModel `json:"data"`
}

// testOpenAIModel mirrors one OpenAI-style model entry returned by the gateway.
type testOpenAIModel struct {
	// ID is the model identifier exposed by the gateway.
	ID string `json:"id"`
	// Providers lists the provider names that can currently route the model.
	Providers []string `json:"providers"`
}

// TestModelAliasesRouteAndRewriteRequests verifies that aliases appear in the
// aggregated model catalog, can be pinned to a provider, and rewrite outbound
// upstream requests to the configured target model.
func TestModelAliasesRouteAndRewriteRequests(t *testing.T) {
	t.Parallel()

	primaryUpstream := newModelAliasUpstreamServer(t, "Primary")
	defer primaryUpstream.Close()

	secondaryUpstream := newModelAliasUpstreamServer(t, "Secondary")
	defer secondaryUpstream.Close()

	gatewayURL, client := newTestGatewayServer(t)
	createNamedTestProvider(t, client, gatewayURL, "Primary", primaryUpstream.URL+"/v1", "PrimaryAgent/1.0")
	secondaryProvider := createNamedTestProvider(t, client, gatewayURL, "Secondary", secondaryUpstream.URL+"/v1", "SecondaryAgent/1.0")
	apiKey := createTestGatewayAPIKey(t, client, gatewayURL, "Alias test key")
	v1Client := newAuthenticatedAPIClient(client, apiKey)

	var createdAliasPayload testModelAliasCreateResponse
	doJSONRequest(
		t,
		client,
		http.MethodPost,
		gatewayURL+"/api/model-aliases",
		`{"aliasModelId":"team-gateway","targetModelId":"gpt-4.1"}`,
		http.StatusCreated,
		&createdAliasPayload,
	)

	if createdAliasPayload.Alias.ID <= 0 {
		t.Fatalf("created alias id = %d, want positive value", createdAliasPayload.Alias.ID)
	}
	if createdAliasPayload.Alias.AliasModelID != "team-gateway" {
		t.Fatalf("created alias model id = %q, want %q", createdAliasPayload.Alias.AliasModelID, "team-gateway")
	}
	if createdAliasPayload.Alias.TargetModelID != "gpt-4.1" {
		t.Fatalf("created alias target model id = %q, want %q", createdAliasPayload.Alias.TargetModelID, "gpt-4.1")
	}
	if !createdAliasPayload.Alias.Routable {
		t.Fatalf("created alias routable = false, want true")
	}
	if len(createdAliasPayload.Alias.Providers) != 2 {
		t.Fatalf("created alias providers = %#v, want both providers", createdAliasPayload.Alias.Providers)
	}

	doJSONRequest(
		t,
		client,
		http.MethodPut,
		gatewayURL+"/api/model-aliases/"+formatInt64(createdAliasPayload.Alias.ID),
		`{"aliasModelId":"team-gateway","targetModelId":"gpt-4.1","targetProviderId":`+formatInt64(secondaryProvider.ID)+`}`,
		http.StatusOK,
		&createdAliasPayload,
	)

	if createdAliasPayload.Alias.TargetProviderID == nil || *createdAliasPayload.Alias.TargetProviderID != secondaryProvider.ID {
		t.Fatalf("updated alias target provider id = %v, want %d", createdAliasPayload.Alias.TargetProviderID, secondaryProvider.ID)
	}
	if createdAliasPayload.Alias.TargetProviderName != "Secondary" {
		t.Fatalf("updated alias target provider name = %q, want %q", createdAliasPayload.Alias.TargetProviderName, "Secondary")
	}
	if len(createdAliasPayload.Alias.Providers) != 1 || createdAliasPayload.Alias.Providers[0].ID != secondaryProvider.ID {
		t.Fatalf("updated alias providers = %#v, want only the secondary provider", createdAliasPayload.Alias.Providers)
	}

	var aliasesPayload testModelAliasesResponse
	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/model-aliases", "", http.StatusOK, &aliasesPayload)
	if len(aliasesPayload.Aliases) != 1 {
		t.Fatalf("alias count = %d, want 1", len(aliasesPayload.Aliases))
	}
	if aliasesPayload.Aliases[0].AliasModelID != "team-gateway" {
		t.Fatalf("listed alias model id = %q, want %q", aliasesPayload.Aliases[0].AliasModelID, "team-gateway")
	}
	if len(aliasesPayload.Aliases[0].Providers) != 1 || aliasesPayload.Aliases[0].Providers[0].Name != "Secondary" {
		t.Fatalf("listed alias providers = %#v, want only the secondary provider", aliasesPayload.Aliases[0].Providers)
	}

	var modelsPayload testOpenAIModelsResponse
	doJSONRequest(t, v1Client, http.MethodGet, gatewayURL+"/v1/models", "", http.StatusOK, &modelsPayload)
	if !containsOpenAIModel(modelsPayload.Data, "team-gateway", []string{"Secondary"}) {
		t.Fatalf("openai models payload = %#v, want team-gateway routed only to Secondary", modelsPayload.Data)
	}

	var chatPayload map[string]any
	doJSONRequest(
		t,
		v1Client,
		http.MethodPost,
		gatewayURL+"/v1/chat/completions",
		`{"model":"team-gateway","messages":[{"role":"user","content":"hello"}]}`,
		http.StatusOK,
		&chatPayload,
	)
	if chatPayload["provider"] != "Secondary" {
		t.Fatalf("chat routed provider = %v, want Secondary", chatPayload["provider"])
	}
	if chatPayload["receivedModel"] != "gpt-4.1" {
		t.Fatalf("chat received model = %v, want gpt-4.1", chatPayload["receivedModel"])
	}

	var modelDetailPayload map[string]any
	doJSONRequest(t, v1Client, http.MethodGet, gatewayURL+"/v1/models/team-gateway", "", http.StatusOK, &modelDetailPayload)
	if modelDetailPayload["provider"] != "Secondary" {
		t.Fatalf("model detail routed provider = %v, want Secondary", modelDetailPayload["provider"])
	}
	if modelDetailPayload["id"] != "gpt-4.1" {
		t.Fatalf("model detail id = %v, want gpt-4.1", modelDetailPayload["id"])
	}

	var logsPayload testProxyRequestsResponse
	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/requests?limit=10", "", http.StatusOK, &logsPayload)
	if len(logsPayload.Requests) < 3 {
		t.Fatalf("request log count = %d, want at least 3", len(logsPayload.Requests))
	}

	chatLog := findRequestLogByPath(logsPayload.Requests, "/v1/chat/completions")
	if chatLog == nil {
		t.Fatalf("request logs = %#v, want a chat-completions audit record", logsPayload.Requests)
	}
	if chatLog.ModelID != "team-gateway" {
		t.Fatalf("chat log model id = %q, want %q", chatLog.ModelID, "team-gateway")
	}
	if chatLog.ProviderName != "Secondary" {
		t.Fatalf("chat log provider name = %q, want %q", chatLog.ProviderName, "Secondary")
	}
	if !strings.Contains(chatLog.ReceivedRequest.Body, `"model":"team-gateway"`) {
		t.Fatalf("chat log received request body = %q, want alias model id", chatLog.ReceivedRequest.Body)
	}
	if chatLog.SentRequest == nil {
		t.Fatalf("chat log sent request = nil, want populated upstream request")
	}
	if !strings.Contains(chatLog.SentRequest.Body, `"model":"gpt-4.1"`) {
		t.Fatalf("chat log sent request body = %q, want target model id", chatLog.SentRequest.Body)
	}

	doJSONRequest(
		t,
		client,
		http.MethodDelete,
		gatewayURL+"/api/model-aliases/"+formatInt64(createdAliasPayload.Alias.ID),
		"",
		http.StatusNoContent,
		nil,
	)

	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/model-aliases", "", http.StatusOK, &aliasesPayload)
	if len(aliasesPayload.Aliases) != 0 {
		t.Fatalf("alias count after delete = %d, want 0", len(aliasesPayload.Aliases))
	}

	doJSONRequest(t, v1Client, http.MethodGet, gatewayURL+"/v1/models", "", http.StatusOK, &modelsPayload)
	if containsOpenAIModel(modelsPayload.Data, "team-gateway", nil) {
		t.Fatalf("openai models payload after delete = %#v, want no team-gateway alias", modelsPayload.Data)
	}
}

// newModelAliasUpstreamServer creates a test upstream that serves one model,
// echoes the received model in chat responses, and supports model-detail lookups.
func newModelAliasUpstreamServer(t *testing.T, providerName string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4.1"}]}`)
		case "/v1/models/gpt-4.1":
			_, _ = io.WriteString(w, `{"id":"gpt-4.1","object":"model","provider":"`+providerName+`"}`)
		case "/v1/chat/completions":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("read upstream request body: %v", err)
				http.Error(w, "upstream body read failed", http.StatusInternalServerError)
				return
			}
			if !strings.Contains(string(body), `"model":"gpt-4.1"`) {
				t.Errorf("upstream request body = %q, want target model", string(body))
				http.Error(w, "missing target model", http.StatusBadRequest)
				return
			}
			if strings.Contains(string(body), `"model":"team-gateway"`) {
				t.Errorf("upstream request body = %q, unexpected alias model", string(body))
				http.Error(w, "unexpected alias model", http.StatusBadRequest)
				return
			}
			_, _ = io.WriteString(w, `{"provider":"`+providerName+`","receivedModel":"gpt-4.1","object":"chat.completion"}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

// containsOpenAIModel reports whether an OpenAI-style model list contains the
// requested model identifier and, when provided, the expected provider names.
func containsOpenAIModel(models []testOpenAIModel, targetID string, wantProviders []string) bool {
	for _, model := range models {
		if model.ID != targetID {
			continue
		}

		if wantProviders == nil {
			return true
		}
		if len(model.Providers) != len(wantProviders) {
			return false
		}
		for index := range wantProviders {
			if model.Providers[index] != wantProviders[index] {
				return false
			}
		}
		return true
	}

	return false
}

// findRequestLogByPath returns the newest request-log record that matches the
// requested inbound path.
func findRequestLogByPath(logs []testProxyRequestLog, targetPath string) *testProxyRequestLog {
	for index := range logs {
		if logs[index].ReceivedRequest.Path == targetPath {
			return &logs[index]
		}
	}

	return nil
}
