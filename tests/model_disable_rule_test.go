package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// testProvidersResponse mirrors the provider-list payload returned by the
// admin API.
type testProvidersResponse struct {
	// Providers contains the saved provider records.
	Providers []testProviderView `json:"providers"`
}

// testModelsResponse mirrors the model-route payload returned by the admin API.
type testModelsResponse struct {
	// Models contains the aggregated model routes that remain routable.
	Models []testModelRoute `json:"models"`
}

// testModelRoute mirrors one aggregated model route returned by the admin API.
type testModelRoute struct {
	// ID is the OpenAI model identifier.
	ID string `json:"id"`
	// Providers contains the enabled providers that still route this model.
	Providers []testModelProviderSummary `json:"providers"`
}

// testModelProviderSummary mirrors one provider summary inside the model-route
// payload returned by the admin API.
type testModelProviderSummary struct {
	// ID is the provider identifier.
	ID int64 `json:"id"`
	// Name is the provider label shown in the dashboard.
	Name string `json:"name"`
}

// TestModelDisableRulesFilterRouting verifies that provider/model disable rules
// remove a pair from routing, from the aggregated model catalog, and that
// removing a rule restores the route.
func TestModelDisableRulesFilterRouting(t *testing.T) {
	t.Parallel()

	primaryUpstream := newModelDisableRuleUpstreamServer(t, "Primary")
	defer primaryUpstream.Close()

	secondaryUpstream := newModelDisableRuleUpstreamServer(t, "Secondary")
	defer secondaryUpstream.Close()

	gatewayURL, client := newTestGatewayServer(t)
	primaryProvider := createNamedTestProvider(t, client, gatewayURL, "Primary", primaryUpstream.URL+"/v1", "PrimaryAgent/1.0")
	secondaryProvider := createNamedTestProvider(t, client, gatewayURL, "Secondary", secondaryUpstream.URL+"/v1", "SecondaryAgent/1.0")
	apiKey := createTestGatewayAPIKey(t, client, gatewayURL, "Disable rule test key")
	v1Client := newAuthenticatedAPIClient(client, apiKey)

	doJSONRequest(
		t,
		client,
		http.MethodPut,
		gatewayURL+"/api/model-disable-rules",
		`{"rules":[{"providerId":`+formatInt64(primaryProvider.ID)+`,"modelId":"gpt-4.1","disabled":true}]}`,
		http.StatusOK,
		nil,
	)

	var providersPayload testProvidersResponse
	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/providers", "", http.StatusOK, &providersPayload)
	if len(providersPayload.Providers) != 2 {
		t.Fatalf("provider count = %d, want 2", len(providersPayload.Providers))
	}

	providerIndex := make(map[string]testProviderView, len(providersPayload.Providers))
	for _, provider := range providersPayload.Providers {
		providerIndex[provider.Name] = provider
	}

	if !containsString(providerIndex["Primary"].DisabledModels, "gpt-4.1") {
		t.Fatalf("primary disabled models = %v, want gpt-4.1", providerIndex["Primary"].DisabledModels)
	}
	if containsString(providerIndex["Secondary"].DisabledModels, "gpt-4.1") {
		t.Fatalf("secondary disabled models = %v, want no gpt-4.1 rule", providerIndex["Secondary"].DisabledModels)
	}

	var modelsPayload testModelsResponse
	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/models", "", http.StatusOK, &modelsPayload)
	if len(modelsPayload.Models) != 1 {
		t.Fatalf("model count = %d, want 1", len(modelsPayload.Models))
	}
	if len(modelsPayload.Models[0].Providers) != 1 || modelsPayload.Models[0].Providers[0].ID != secondaryProvider.ID {
		t.Fatalf("model providers = %#v, want only secondary provider", modelsPayload.Models[0].Providers)
	}

	var routedPayload map[string]any
	doJSONRequest(
		t,
		v1Client,
		http.MethodPost,
		gatewayURL+"/v1/chat/completions",
		`{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`,
		http.StatusOK,
		&routedPayload,
	)
	if routedPayload["provider"] != "Secondary" {
		t.Fatalf("routed provider = %v, want Secondary", routedPayload["provider"])
	}

	doJSONRequest(
		t,
		client,
		http.MethodPut,
		gatewayURL+"/api/model-disable-rules",
		`{"rules":[{"providerId":`+formatInt64(secondaryProvider.ID)+`,"modelId":"gpt-4.1","disabled":true}]}`,
		http.StatusOK,
		nil,
	)

	var errorPayload map[string]any
	doJSONRequest(
		t,
		v1Client,
		http.MethodPost,
		gatewayURL+"/v1/chat/completions",
		`{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`,
		http.StatusNotFound,
		&errorPayload,
	)
	if !strings.Contains(errorPayload["error"].(string), `no enabled provider serves model "gpt-4.1"`) {
		t.Fatalf("error payload = %v, want disabled-provider message", errorPayload)
	}

	doJSONRequest(
		t,
		client,
		http.MethodPut,
		gatewayURL+"/api/model-disable-rules",
		`{"rules":[{"providerId":`+formatInt64(primaryProvider.ID)+`,"modelId":"gpt-4.1","disabled":false}]}`,
		http.StatusOK,
		nil,
	)

	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/providers", "", http.StatusOK, &providersPayload)
	providerIndex = make(map[string]testProviderView, len(providersPayload.Providers))
	for _, provider := range providersPayload.Providers {
		providerIndex[provider.Name] = provider
	}

	if containsString(providerIndex["Primary"].DisabledModels, "gpt-4.1") {
		t.Fatalf("primary disabled models after removal = %v, want no gpt-4.1 rule", providerIndex["Primary"].DisabledModels)
	}
	if !containsString(providerIndex["Secondary"].DisabledModels, "gpt-4.1") {
		t.Fatalf("secondary disabled models after add = %v, want gpt-4.1", providerIndex["Secondary"].DisabledModels)
	}

	doJSONRequest(
		t,
		v1Client,
		http.MethodPost,
		gatewayURL+"/v1/chat/completions",
		`{"model":"gpt-4.1","messages":[{"role":"user","content":"hello"}]}`,
		http.StatusOK,
		&routedPayload,
	)
	if routedPayload["provider"] != "Primary" {
		t.Fatalf("routed provider after rule removal = %v, want Primary", routedPayload["provider"])
	}
}

// TestModelDisableRulesBatchApply verifies that one batch request can apply
// multiple disable-rule mutations, including a mix of disable and re-enable
// operations.
func TestModelDisableRulesBatchApply(t *testing.T) {
	t.Parallel()

	primaryUpstream := newModelDisableRuleUpstreamServer(t, "Primary")
	defer primaryUpstream.Close()

	secondaryUpstream := newModelDisableRuleUpstreamServer(t, "Secondary")
	defer secondaryUpstream.Close()

	gatewayURL, client := newTestGatewayServer(t)
	primaryProvider := createNamedTestProvider(t, client, gatewayURL, "Primary", primaryUpstream.URL+"/v1", "PrimaryAgent/1.0")
	secondaryProvider := createNamedTestProvider(t, client, gatewayURL, "Secondary", secondaryUpstream.URL+"/v1", "SecondaryAgent/1.0")

	doJSONRequest(
		t,
		client,
		http.MethodPut,
		gatewayURL+"/api/model-disable-rules",
		`{"rules":[{"providerId":`+formatInt64(primaryProvider.ID)+`,"modelId":"gpt-4.1","disabled":true}]}`,
		http.StatusOK,
		nil,
	)

	doJSONRequest(
		t,
		client,
		http.MethodPut,
		gatewayURL+"/api/model-disable-rules",
		`{"rules":[`+
			`{"providerId":`+formatInt64(primaryProvider.ID)+`,"modelId":"gpt-4.1","disabled":false},`+
			`{"providerId":`+formatInt64(secondaryProvider.ID)+`,"modelId":"gpt-4.1","disabled":true}`+
			`]}`,
		http.StatusOK,
		nil,
	)

	var providersPayload testProvidersResponse
	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/providers", "", http.StatusOK, &providersPayload)
	if len(providersPayload.Providers) != 2 {
		t.Fatalf("provider count = %d, want 2", len(providersPayload.Providers))
	}

	providerIndex := make(map[string]testProviderView, len(providersPayload.Providers))
	for _, provider := range providersPayload.Providers {
		providerIndex[provider.Name] = provider
	}

	if containsString(providerIndex["Primary"].DisabledModels, "gpt-4.1") {
		t.Fatalf("primary disabled models after batch = %v, want no gpt-4.1 rule", providerIndex["Primary"].DisabledModels)
	}
	if !containsString(providerIndex["Secondary"].DisabledModels, "gpt-4.1") {
		t.Fatalf("secondary disabled models after batch = %v, want gpt-4.1", providerIndex["Secondary"].DisabledModels)
	}
}

// newModelDisableRuleUpstreamServer creates a test upstream that serves one
// model and returns its provider label in chat-completion responses.
func newModelDisableRuleUpstreamServer(t *testing.T, providerName string) *httptest.Server {
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
			_, _ = io.WriteString(w, `{"provider":"`+providerName+`","model":"gpt-4.1","object":"chat.completion"}`)
		default:
			http.NotFound(w, r)
		}
	}))
}

// containsString reports whether a string slice contains the requested value.
func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// formatInt64 converts an int64 into decimal text for inline JSON request
// bodies used by the integration tests.
func formatInt64(value int64) string {
	return strconv.FormatInt(value, 10)
}
