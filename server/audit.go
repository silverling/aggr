package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// defaultProxyRequestLogLimit is the number of request log rows returned when
	// the caller does not specify a limit.
	defaultProxyRequestLogLimit = 25
	// maxProxyRequestLogLimit is the largest request log page size accepted by the API.
	maxProxyRequestLogLimit = 100
	// maxAuditBodyBytes bounds the amount of request or response body text stored
	// for one audit row so the database and UI remain responsive.
	maxAuditBodyBytes = 1 << 20
)

// proxyResponseCapture contains the response metadata and body preview collected
// while the gateway streams an upstream provider response to the client.
type proxyResponseCapture struct {
	// StatusCode is the HTTP status returned to the client.
	StatusCode int
	// HeadersJSON is the serialized response header map after gateway headers are applied.
	HeadersJSON string
	// Body is the captured response body text, truncated when it exceeds the audit limit.
	Body string
	// BodyTruncated reports whether the stored response body preview was shortened.
	BodyTruncated bool
	// StreamError captures any copy error encountered while streaming to the caller.
	StreamError error
}

// cappedBuffer records up to a fixed number of bytes while reporting whether any
// additional content had to be discarded.
type cappedBuffer struct {
	limit     int
	data      []byte
	truncated bool
}

// newCappedBuffer constructs a cappedBuffer with the provided byte limit.
func newCappedBuffer(limit int) *cappedBuffer {
	return &cappedBuffer{
		limit: limit,
		data:  make([]byte, 0, limit),
	}
}

// Write appends bytes until the configured limit is reached and then marks the
// buffer as truncated while still reporting the full write length to the caller.
func (buffer *cappedBuffer) Write(p []byte) (int, error) {
	remaining := buffer.limit - len(buffer.data)
	if remaining <= 0 {
		buffer.truncated = true
		return len(p), nil
	}

	if len(p) > remaining {
		buffer.data = append(buffer.data, p[:remaining]...)
		buffer.truncated = true
		return len(p), nil
	}

	buffer.data = append(buffer.data, p...)
	return len(p), nil
}

// String returns the captured bytes as a string for storage in SQLite and the UI.
func (buffer *cappedBuffer) String() string {
	return string(buffer.data)
}

// Truncated reports whether the original byte stream exceeded the configured cap.
func (buffer *cappedBuffer) Truncated() bool {
	return buffer.truncated
}

// createProxyRequestAudit stores the inbound request metadata and returns the
// inserted log row identifier, or zero when persistence failed.
func (s *server) createProxyRequestAudit(ctx context.Context, r *http.Request, modelID string, requestBody []byte) int64 {
	body, truncated := truncateAuditBytes(requestBody)
	logID, err := s.store.createProxyRequestLog(ctx, proxyRequestLogCreate{
		Method:               r.Method,
		Path:                 r.URL.Path,
		RawQuery:             r.URL.RawQuery,
		ModelID:              strings.TrimSpace(modelID),
		RequestHeaders:       headersToAuditJSON(r.Header),
		RequestBody:          body,
		RequestBodyTruncated: truncated,
		RequestedAt:          time.Now(),
	})
	if err != nil {
		s.logger.Error("create proxy request audit", "method", r.Method, "path", r.URL.Path, "error", err)
		return 0
	}

	return logID
}

// completeProxyRequestAudit updates a previously inserted audit row and logs any
// persistence failure without affecting the client response path.
func (s *server) completeProxyRequestAudit(ctx context.Context, logID int64, update proxyRequestLogUpdate) {
	if logID == 0 {
		return
	}

	if err := s.store.completeProxyRequestLog(ctx, logID, update); err != nil {
		s.logger.Error("complete proxy request audit", "log_id", logID, "error", err)
	}
}

// writeLoggedProxyError writes a JSON error response to the caller and records
// the same payload in the audit log with the matching HTTP status code.
func (s *server) writeLoggedProxyError(w http.ResponseWriter, ctx context.Context, logID int64, startedAt time.Time, provider *providerRecord, sentRequest *proxyRequestSentCapture, status int, message string) {
	body, err := encodeJSONPayload(map[string]string{
		"error": message,
	})
	if err != nil {
		body = []byte(fmt.Sprintf(`{"error":%q}`, message))
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	update := proxyRequestLogUpdate{
		ResponseStatus:        status,
		ResponseHeaders:       headersToAuditJSON(w.Header()),
		ResponseBody:          string(body),
		ResponseBodyTruncated: false,
		ErrorText:             message,
		DurationMS:            time.Since(startedAt).Milliseconds(),
		CompletedAt:           time.Now(),
	}
	if sentRequest != nil {
		update.SentMethod = sentRequest.Method
		update.SentURL = sentRequest.URL
		update.SentHeaders = sentRequest.HeadersJSON
		update.SentBody = sentRequest.Body
		update.SentBodyTruncated = sentRequest.BodyTruncated
	}
	if provider != nil {
		update.ProviderID = &provider.ID
		update.ProviderName = provider.Name
	}

	s.completeProxyRequestAudit(ctx, logID, update)
	writeJSONBytes(w, status, body)
}

// truncateAuditBytes converts a byte slice into stored text and reports whether
// the body preview had to be shortened to fit the audit size limit.
func truncateAuditBytes(body []byte) (string, bool) {
	if len(body) <= maxAuditBodyBytes {
		return string(body), false
	}
	return string(body[:maxAuditBodyBytes]), true
}

// headersToAuditJSON serializes headers into readable JSON while redacting
// caller secrets such as Authorization and cookie values.
func headersToAuditJSON(headers http.Header) string {
	sanitized := make(map[string][]string, len(headers))
	for key, values := range headers {
		lower := strings.ToLower(key)
		switch lower {
		case "authorization", "cookie", "set-cookie":
			sanitized[key] = []string{"[redacted]"}
		default:
			sanitized[key] = append([]string(nil), values...)
		}
	}

	body, err := json.MarshalIndent(sanitized, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(body)
}

// encodeJSONPayload marshals a JSON response body so callers can both write it
// to the client and persist the exact bytes in the audit log.
func encodeJSONPayload(payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode json payload: %w", err)
	}
	return body, nil
}

// normalizeProxyRequestLogLimit clamps the caller-provided page size into the
// safe range supported by the request log API.
func normalizeProxyRequestLogLimit(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return defaultProxyRequestLogLimit
	}

	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value <= 0 {
		return defaultProxyRequestLogLimit
	}
	if value > maxProxyRequestLogLimit {
		return maxProxyRequestLogLimit
	}
	return value
}
