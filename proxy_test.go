package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveProviderURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		baseURL  string
		path     string
		rawQuery string
		wantURL  string
	}{
		{
			name:    "host root keeps v1 path",
			baseURL: "https://api.openai.com",
			path:    "/v1/chat/completions",
			wantURL: "https://api.openai.com/v1/chat/completions",
		},
		{
			name:    "host with v1 avoids duplicate prefix",
			baseURL: "https://api.openai.com/v1",
			path:    "/v1/models",
			wantURL: "https://api.openai.com/v1/models",
		},
		{
			name:     "preserves provider prefix path",
			baseURL:  "https://example.com/openai/v1",
			path:     "/v1/responses",
			rawQuery: "stream=true",
			wantURL:  "https://example.com/openai/v1/responses?stream=true",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, err := resolveProviderURL(test.baseURL, test.path, test.rawQuery)
			if err != nil {
				t.Fatalf("resolveProviderURL() error = %v", err)
			}
			if got != test.wantURL {
				t.Fatalf("resolveProviderURL() = %q, want %q", got, test.wantURL)
			}
		})
	}
}

func TestExtractModelHint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		target    string
		body      string
		wantModel string
		wantErr   bool
	}{
		{
			name:      "extracts model from body",
			target:    "/v1/chat/completions",
			body:      `{"model":"gpt-4.1","messages":[]}`,
			wantModel: "gpt-4.1",
		},
		{
			name:      "extracts model from path",
			target:    "/v1/models/o3",
			wantModel: "o3",
		},
		{
			name:    "fails without model",
			target:  "/v1/chat/completions",
			body:    `{"messages":[]}`,
			wantErr: true,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(http.MethodPost, test.target, http.NoBody)
			if test.body != "" {
				request = httptest.NewRequest(http.MethodPost, test.target, strings.NewReader(test.body))
				request.Header.Set("Content-Type", "application/json")
			}

			model, _, err := extractModelHint(request)
			if test.wantErr {
				if err == nil {
					t.Fatalf("extractModelHint() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("extractModelHint() error = %v", err)
			}
			if model != test.wantModel {
				t.Fatalf("extractModelHint() = %q, want %q", model, test.wantModel)
			}
		})
	}
}
