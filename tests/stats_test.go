package server_test

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// testRequestStatsResponse mirrors the stats payload returned by the dashboard API.
type testRequestStatsResponse struct {
	// Range is the canonical selector used for the summary window.
	Range string `json:"range"`
	// RangeLabel is the human-readable label for the selected summary window.
	RangeLabel string `json:"rangeLabel"`
	// Summary contains the top-line request and token counts.
	Summary testRequestStatsSummary `json:"summary"`
	// Daily contains the recent 7-day token-usage chart buckets.
	Daily []testRequestStatsBucket `json:"daily"`
	// Hourly contains the recent 12-hour token-usage chart buckets.
	Hourly []testRequestStatsBucket `json:"hourly"`
}

// testRequestStatsSummary mirrors the summary portion of the stats payload.
type testRequestStatsSummary struct {
	// Requests is the total number of audited requests in the selected range.
	Requests int64 `json:"requests"`
	// Succeeded is the number of completed 2xx requests in the selected range.
	Succeeded int64 `json:"succeeded"`
	// Failed is the number of completed non-2xx requests in the selected range.
	Failed int64 `json:"failed"`
	// ConsumedTokens is the total of input plus output tokens.
	ConsumedTokens int64 `json:"consumedTokens"`
	// CachedInputTokens is the number of cached input tokens.
	CachedInputTokens int64 `json:"cachedInputTokens"`
	// NonCachedInputTokens is the number of non-cached input tokens.
	NonCachedInputTokens int64 `json:"nonCachedInputTokens"`
	// OutputTokens is the number of output tokens.
	OutputTokens int64 `json:"outputTokens"`
	// OngoingRequests is the number of currently unfinished requests.
	OngoingRequests int64 `json:"ongoingRequests"`
}

// testRequestStatsBucket mirrors one chart bucket in the stats payload.
type testRequestStatsBucket struct {
	// Start is the UTC bucket start timestamp.
	Start string `json:"start"`
	// Label is the human-readable label shown in the chart.
	Label string `json:"label"`
	// Requests is the number of requests that started in the bucket.
	Requests int64 `json:"requests"`
	// Succeeded is the number of completed 2xx requests in the bucket.
	Succeeded int64 `json:"succeeded"`
	// Failed is the number of completed non-2xx requests in the bucket.
	Failed int64 `json:"failed"`
	// ConsumedTokens is the sum of input and output tokens in the bucket.
	ConsumedTokens int64 `json:"consumedTokens"`
	// CachedInputTokens is the number of cached input tokens in the bucket.
	CachedInputTokens int64 `json:"cachedInputTokens"`
	// NonCachedInputTokens is the number of non-cached input tokens in the bucket.
	NonCachedInputTokens int64 `json:"nonCachedInputTokens"`
	// OutputTokens is the number of output tokens in the bucket.
	OutputTokens int64 `json:"outputTokens"`
}

// testProxyRequestLogSeed describes one audit-row fixture inserted directly
// into SQLite for stats-oriented integration tests.
type testProxyRequestLogSeed struct {
	// Method is the inbound HTTP verb stored in the audit row.
	Method string
	// Path is the inbound request path stored in the audit row.
	Path string
	// ModelID is the model identifier associated with the request.
	ModelID string
	// ResponseStatus is the optional final HTTP status code.
	ResponseStatus *int
	// ResponseBody is the stored response body payload.
	ResponseBody string
	// CachedInputTokens is the persisted cached-input token count for the row.
	CachedInputTokens int64
	// NonCachedInputTokens is the persisted non-cached input token count for the row.
	NonCachedInputTokens int64
	// OutputTokens is the persisted output token count for the row.
	OutputTokens int64
	// TotalTokens is the persisted total token count for the row.
	TotalTokens int64
	// RequestedAt is when the request started.
	RequestedAt time.Time
	// CompletedAt is when the request finished, or nil when it is still ongoing.
	CompletedAt *time.Time
}

// TestRequestStatsSummariesAndCharts verifies that the stats endpoint reports
// range-filtered request counts, ongoing requests, token totals, and the fixed
// daily and hourly chart buckets derived from the audit log.
func TestRequestStatsSummariesAndCharts(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")

		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4.1"}]}`)
		case "/v1/chat/completions":
			_, _ = io.WriteString(w, `{"object":"chat.completion","usage":{"prompt_tokens":10,"completion_tokens":4,"prompt_tokens_details":{"cached_tokens":3}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	gatewayURL, client, db := newTestGatewayServerWithDatabase(t)
	createTestProvider(t, client, gatewayURL, upstream.URL+"/v1", "StatsAgent/1.0")
	apiKey := createTestGatewayAPIKey(t, client, gatewayURL, "Stats test key")
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
	doJSONRequest(
		t,
		v1Client,
		http.MethodPost,
		gatewayURL+"/v1/chat/completions",
		`{"model":"missing-model","messages":[{"role":"user","content":"hello"}]}`,
		http.StatusNotFound,
		nil,
	)

	now := time.Now().UTC()
	fiveHoursAgoRequestedAt := now.Add(-5*time.Hour + 1*time.Minute)
	fiveHoursAgoCompletedAt := now.Add(-5*time.Hour + 3*time.Minute)
	twoDaysAgoCompletedAt := now.AddDate(0, 0, -2).Add(11 * time.Minute)
	insertTestProxyRequestLog(t, db, testProxyRequestLogSeed{
		Method:               http.MethodPost,
		Path:                 "/v1/responses",
		ModelID:              "gpt-4.1",
		ResponseStatus:       intPointer(http.StatusOK),
		ResponseBody:         `{"usage":{"input_tokens":8,"output_tokens":3,"input_tokens_details":{"cached_tokens":2}}}`,
		CachedInputTokens:    2,
		NonCachedInputTokens: 6,
		OutputTokens:         3,
		TotalTokens:          11,
		RequestedAt:          fiveHoursAgoRequestedAt,
		CompletedAt:          &fiveHoursAgoCompletedAt,
	})
	insertTestProxyRequestLog(t, db, testProxyRequestLogSeed{
		Method:               http.MethodPost,
		Path:                 "/v1/chat/completions",
		ModelID:              "gpt-4.1",
		ResponseStatus:       intPointer(http.StatusOK),
		ResponseBody:         `{"usage":{"prompt_tokens":18,"completion_tokens":6,"prompt_tokens_details":{"cached_tokens":5}}}`,
		CachedInputTokens:    5,
		NonCachedInputTokens: 13,
		OutputTokens:         6,
		TotalTokens:          24,
		RequestedAt:          now.AddDate(0, 0, -2),
		CompletedAt:          &twoDaysAgoCompletedAt,
	})
	insertTestProxyRequestLog(t, db, testProxyRequestLogSeed{
		Method:       http.MethodPost,
		Path:         "/v1/chat/completions",
		ModelID:      "gpt-4.1",
		ResponseBody: "",
		RequestedAt:  now,
		CompletedAt:  nil,
	})

	var allStats testRequestStatsResponse
	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/stats?range=all", "", http.StatusOK, &allStats)

	if allStats.Range != "all" {
		t.Fatalf("all stats range = %q, want %q", allStats.Range, "all")
	}
	if allStats.Summary.Requests != 5 {
		t.Fatalf("all stats request count = %d, want 5", allStats.Summary.Requests)
	}
	if allStats.Summary.Succeeded != 3 {
		t.Fatalf("all stats succeeded = %d, want 3", allStats.Summary.Succeeded)
	}
	if allStats.Summary.Failed != 1 {
		t.Fatalf("all stats failed = %d, want 1", allStats.Summary.Failed)
	}
	if allStats.Summary.ConsumedTokens != 49 {
		t.Fatalf("all stats consumed tokens = %d, want 49", allStats.Summary.ConsumedTokens)
	}
	if allStats.Summary.CachedInputTokens != 10 {
		t.Fatalf("all stats cached input tokens = %d, want 10", allStats.Summary.CachedInputTokens)
	}
	if allStats.Summary.NonCachedInputTokens != 26 {
		t.Fatalf("all stats non-cached input tokens = %d, want 26", allStats.Summary.NonCachedInputTokens)
	}
	if allStats.Summary.OutputTokens != 13 {
		t.Fatalf("all stats output tokens = %d, want 13", allStats.Summary.OutputTokens)
	}
	if allStats.Summary.OngoingRequests != 1 {
		t.Fatalf("all stats ongoing requests = %d, want 1", allStats.Summary.OngoingRequests)
	}

	currentDayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	twoDaysAgoStart := currentDayStart.AddDate(0, 0, -2)
	fiveHoursAgoDayStart := time.Date(fiveHoursAgoRequestedAt.Year(), fiveHoursAgoRequestedAt.Month(), fiveHoursAgoRequestedAt.Day(), 0, 0, 0, 0, time.UTC)
	fiveHoursAgoSameDay := fiveHoursAgoDayStart.Equal(currentDayStart)
	currentDayBucket := findStatsBucketByStart(t, allStats.Daily, currentDayStart)
	if currentDayBucket == nil {
		t.Fatalf("daily chart missing current-day bucket: %#v", allStats.Daily)
	}
	wantCurrentDayRequests := int64(3)
	if fiveHoursAgoSameDay {
		wantCurrentDayRequests = 4
	}
	if currentDayBucket.Requests != wantCurrentDayRequests {
		t.Fatalf("current-day requests = %d, want %d", currentDayBucket.Requests, wantCurrentDayRequests)
	}
	wantCurrentDaySucceeded := int64(1)
	if fiveHoursAgoSameDay {
		wantCurrentDaySucceeded = 2
	}
	if currentDayBucket.Succeeded != wantCurrentDaySucceeded {
		t.Fatalf("current-day succeeded = %d, want %d", currentDayBucket.Succeeded, wantCurrentDaySucceeded)
	}
	if currentDayBucket.Failed != 1 {
		t.Fatalf("current-day failed = %d, want 1", currentDayBucket.Failed)
	}
	wantCurrentDayConsumedTokens := int64(14)
	if fiveHoursAgoSameDay {
		wantCurrentDayConsumedTokens = 25
	}
	if currentDayBucket.ConsumedTokens != wantCurrentDayConsumedTokens {
		t.Fatalf("current-day consumed tokens = %d, want %d", currentDayBucket.ConsumedTokens, wantCurrentDayConsumedTokens)
	}

	twoDaysAgoBucket := findStatsBucketByStart(t, allStats.Daily, twoDaysAgoStart)
	if twoDaysAgoBucket == nil {
		t.Fatalf("daily chart missing two-days-ago bucket: %#v", allStats.Daily)
	}
	if twoDaysAgoBucket.Requests != 1 || twoDaysAgoBucket.ConsumedTokens != 24 {
		t.Fatalf("two-days-ago bucket = %#v, want 1 request and 24 tokens", *twoDaysAgoBucket)
	}

	currentHourStart := now.Truncate(time.Hour)
	fiveHoursAgoStart := fiveHoursAgoRequestedAt.Truncate(time.Hour)
	currentHourBucket := findStatsBucketByStart(t, allStats.Hourly, currentHourStart)
	if currentHourBucket == nil {
		t.Fatalf("hourly chart missing current-hour bucket: %#v", allStats.Hourly)
	}
	if currentHourBucket.Requests != 3 {
		t.Fatalf("current-hour requests = %d, want 3", currentHourBucket.Requests)
	}
	if currentHourBucket.Succeeded != 1 || currentHourBucket.Failed != 1 {
		t.Fatalf("current-hour outcome counts = %#v, want 1 success and 1 failure", *currentHourBucket)
	}
	if currentHourBucket.ConsumedTokens != 14 {
		t.Fatalf("current-hour consumed tokens = %d, want 14", currentHourBucket.ConsumedTokens)
	}

	fiveHoursAgoBucket := findStatsBucketByStart(t, allStats.Hourly, fiveHoursAgoStart)
	if fiveHoursAgoBucket == nil {
		t.Fatalf("hourly chart missing five-hours-ago bucket: %#v", allStats.Hourly)
	}
	if fiveHoursAgoBucket.Requests != 1 || fiveHoursAgoBucket.ConsumedTokens != 11 {
		t.Fatalf("five-hours-ago bucket = %#v, want 1 request and 11 tokens", *fiveHoursAgoBucket)
	}

	var hourStats testRequestStatsResponse
	doJSONRequest(t, client, http.MethodGet, gatewayURL+"/api/stats?range=1h", "", http.StatusOK, &hourStats)
	if hourStats.Range != "1h" {
		t.Fatalf("hour stats range = %q, want %q", hourStats.Range, "1h")
	}
	if hourStats.Summary.Requests != 3 {
		t.Fatalf("hour stats request count = %d, want 3", hourStats.Summary.Requests)
	}
	if hourStats.Summary.Succeeded != 1 {
		t.Fatalf("hour stats succeeded = %d, want 1", hourStats.Summary.Succeeded)
	}
	if hourStats.Summary.Failed != 1 {
		t.Fatalf("hour stats failed = %d, want 1", hourStats.Summary.Failed)
	}
	if hourStats.Summary.ConsumedTokens != 14 {
		t.Fatalf("hour stats consumed tokens = %d, want 14", hourStats.Summary.ConsumedTokens)
	}
	if hourStats.Summary.CachedInputTokens != 3 {
		t.Fatalf("hour stats cached input tokens = %d, want 3", hourStats.Summary.CachedInputTokens)
	}
	if hourStats.Summary.NonCachedInputTokens != 7 {
		t.Fatalf("hour stats non-cached input tokens = %d, want 7", hourStats.Summary.NonCachedInputTokens)
	}
	if hourStats.Summary.OutputTokens != 4 {
		t.Fatalf("hour stats output tokens = %d, want 4", hourStats.Summary.OutputTokens)
	}
	if hourStats.Summary.OngoingRequests != 1 {
		t.Fatalf("hour stats ongoing requests = %d, want 1", hourStats.Summary.OngoingRequests)
	}
}

// insertTestProxyRequestLog inserts one audit-row fixture directly into SQLite
// so integration tests can exercise stats scenarios that are awkward to produce
// through live HTTP traffic alone.
func insertTestProxyRequestLog(t *testing.T, db *sql.DB, seed testProxyRequestLogSeed) {
	t.Helper()

	var responseStatus any
	if seed.ResponseStatus != nil {
		responseStatus = *seed.ResponseStatus
	}

	var completedAt any
	if seed.CompletedAt != nil {
		completedAt = seed.CompletedAt.UTC().Format(time.RFC3339)
	}

	_, err := db.Exec(`
		INSERT INTO proxy_request_logs (
			provider_id, provider_name, model_id, method, path, raw_query,
			request_headers, request_body, request_body_truncated,
			sent_request_method, sent_request_url, sent_request_headers, sent_request_body, sent_request_body_truncated,
			response_status, response_headers, response_body, response_body_truncated,
			error_text, duration_ms, cached_input_tokens, non_cached_input_tokens, output_tokens, total_tokens, requested_at, completed_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		nil,
		"",
		seed.ModelID,
		seed.Method,
		seed.Path,
		"",
		"{}",
		"{}",
		0,
		"",
		"",
		"",
		"",
		0,
		responseStatus,
		"{}",
		seed.ResponseBody,
		0,
		"",
		nil,
		seed.CachedInputTokens,
		seed.NonCachedInputTokens,
		seed.OutputTokens,
		seed.TotalTokens,
		seed.RequestedAt.UTC().Format(time.RFC3339),
		completedAt,
	)
	if err != nil {
		t.Fatalf("insert proxy request log fixture: %v", err)
	}
}

// findStatsBucketByStart returns the bucket whose start timestamp matches the
// provided UTC time.
func findStatsBucketByStart(t *testing.T, buckets []testRequestStatsBucket, wantStart time.Time) *testRequestStatsBucket {
	t.Helper()

	want := wantStart.UTC().Format(time.RFC3339)
	for index := range buckets {
		if buckets[index].Start == want {
			return &buckets[index]
		}
	}

	return nil
}

// intPointer allocates an integer pointer for concise test fixture setup.
func intPointer(value int) *int {
	return &value
}
