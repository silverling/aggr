package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const maxProxyBodyBytes = 32 << 20

var errModelHintMissing = errors.New("model hint missing")

// providerModelListResponse mirrors the upstream `/v1/models` payload.
type providerModelListResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// openAIModelListResponse is the OpenAI-style aggregated models list returned by the gateway.
type openAIModelListResponse struct {
	Object string                `json:"object"`
	Data   []openAIModelResponse `json:"data"`
}

// openAIModelResponse is one entry in the aggregated models list.
type openAIModelResponse struct {
	ID        string   `json:"id"`
	Object    string   `json:"object"`
	Created   int64    `json:"created"`
	OwnedBy   string   `json:"owned_by"`
	Providers []string `json:"providers,omitempty"`
}

// flushingWriter wraps a response writer so long-lived streamed responses flush
// after each copied chunk reaches the caller.
type flushingWriter struct {
	// writer receives the proxied bytes.
	writer io.Writer
	// flusher exposes the HTTP flush primitive for streamed responses.
	flusher http.Flusher
}

// Write forwards the bytes to the client and immediately flushes them so
// streamed provider responses remain responsive.
func (writer *flushingWriter) Write(p []byte) (int, error) {
	written, err := writer.writer.Write(p)
	if err != nil {
		return written, err
	}

	writer.flusher.Flush()
	return written, nil
}

// normalizeBaseURL validates and canonicalizes a provider base URL before it is stored.
func normalizeBaseURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", errors.New("base URL is required")
	}

	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("base URL must include http(s) scheme and host")
	}

	parsed.Fragment = ""
	parsed.RawFragment = ""
	parsed.RawQuery = ""
	if parsed.Path == "/" {
		parsed.Path = ""
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")

	return parsed.String(), nil
}

// resolveProviderPath strips any duplicated `/v1` prefix from the request path for a provider URL.
func resolveProviderPath(baseURL, requestPath string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse provider URL: %w", err)
	}

	basePath := strings.Trim(strings.TrimSuffix(parsed.Path, "/"), "/")
	baseEndsWithV1 := basePath == "v1" || strings.HasSuffix(basePath, "/v1")
	upstreamPath := strings.TrimPrefix(requestPath, "/")
	if baseEndsWithV1 && strings.HasPrefix(upstreamPath, "v1/") {
		upstreamPath = strings.TrimPrefix(upstreamPath, "v1/")
	}
	if upstreamPath == "v1" && baseEndsWithV1 {
		upstreamPath = ""
	}

	return upstreamPath, nil
}

// ResolveProviderURL joins a provider base URL with a gateway request path and query string.
func ResolveProviderURL(baseURL, requestPath string, rawQuery string) (string, error) {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse provider URL: %w", err)
	}

	if parsed.Path != "" && !strings.HasSuffix(parsed.Path, "/") {
		parsed.Path += "/"
	}

	relativePath, err := resolveProviderPath(baseURL, requestPath)
	if err != nil {
		return "", err
	}

	resolved := parsed.ResolveReference(&url.URL{Path: relativePath})
	if strings.HasSuffix(requestPath, "/") && !strings.HasSuffix(resolved.Path, "/") {
		resolved.Path += "/"
	}
	resolved.RawPath = resolved.EscapedPath()
	resolved.RawQuery = rawQuery

	return resolved.String(), nil
}

// newProviderClient creates an OpenAI SDK client configured for one provider and a specific HTTP transport.
func (s *server) newProviderClient(provider providerRecord, httpClient *http.Client) openai.Client {
	options := []option.RequestOption{
		option.WithBaseURL(provider.BaseURL),
		option.WithAPIKey(provider.APIKey),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(0),
	}
	if provider.UserAgent != "" {
		options = append(options, option.WithHeader("User-Agent", provider.UserAgent))
	}

	return openai.Client{
		Options: options,
	}
}

// buildProviderRequestOptions copies inbound headers and query parameters into SDK request options.
func buildProviderRequestOptions(headers http.Header, query url.Values) []option.RequestOption {
	options := make([]option.RequestOption, 0, len(headers)+len(query))

	for key, values := range headers {
		if isHopByHopHeader(key) || strings.EqualFold(key, "Authorization") || strings.EqualFold(key, "Host") || strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "User-Agent") {
			continue
		}

		options = append(options, option.WithHeaderDel(key))
		for _, value := range values {
			options = append(options, option.WithHeaderAdd(key, value))
		}
	}

	for key, values := range query {
		for _, value := range values {
			options = append(options, option.WithQueryAdd(key, value))
		}
	}

	return options
}

// copyUpstreamResponse streams an upstream HTTP response to the caller while
// capturing the final headers and a capped body preview for audit logging.
func copyUpstreamResponse(w http.ResponseWriter, response *http.Response, providerName string, modelID string, logger *slog.Logger) proxyResponseCapture {
	defer response.Body.Close()

	copyResponseHeaders(w.Header(), response.Header)
	w.Header().Set("X-Aggr-Provider", providerName)
	if modelID != "" {
		w.Header().Set("X-Aggr-Model", modelID)
	}
	w.WriteHeader(response.StatusCode)

	streamWriter := io.Writer(w)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
		streamWriter = &flushingWriter{
			writer:  w,
			flusher: flusher,
		}
	}

	bodyBuffer := newCappedBuffer(maxAuditBodyBytes)
	_, streamErr := io.Copy(io.MultiWriter(streamWriter, bodyBuffer), response.Body)
	if streamErr != nil {
		logger.Warn("stream upstream response", "provider", providerName, "model", modelID, "error", streamErr)
	}

	return proxyResponseCapture{
		StatusCode:    response.StatusCode,
		HeadersJSON:   headersToAuditJSON(w.Header()),
		Body:          bodyBuffer.String(),
		BodyTruncated: bodyBuffer.Truncated(),
		StreamError:   streamErr,
	}
}

// ExtractModelHint finds the target model from the request path, query string, or JSON body.
func ExtractModelHint(r *http.Request) (string, []byte, error) {
	if strings.HasPrefix(r.URL.Path, "/v1/models/") {
		modelID := strings.TrimPrefix(r.URL.Path, "/v1/models/")
		return modelID, nil, nil
	}

	body, err := readRequestBody(r)
	if err != nil {
		return "", nil, err
	}

	if modelID := strings.TrimSpace(r.URL.Query().Get("model")); modelID != "" {
		return modelID, body, nil
	}

	if len(body) == 0 {
		return "", body, errModelHintMissing
	}
	if !json.Valid(body) {
		return "", body, errModelHintMissing
	}

	var payload struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", body, fmt.Errorf("decode request body: %w", err)
	}
	if strings.TrimSpace(payload.Model) == "" {
		return "", body, errModelHintMissing
	}

	return payload.Model, body, nil
}

// readRequestBody reads and bounds the inbound body so it can be reused for proxying.
func readRequestBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	defer r.Body.Close()

	body, err := io.ReadAll(io.LimitReader(r.Body, maxProxyBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	if len(body) > maxProxyBodyBytes {
		return nil, fmt.Errorf("request body exceeds %d bytes", maxProxyBodyBytes)
	}
	return body, nil
}

// syncProviderCatalog refreshes one provider's model list using the OpenAI SDK.
func (s *server) syncProviderCatalog(ctx context.Context, provider providerRecord) error {
	path, err := resolveProviderPath(provider.BaseURL, "/v1/models")
	if err != nil {
		persistErr := s.store.setProviderSyncError(ctx, provider.ID, err)
		if persistErr != nil {
			s.logger.Error("persist provider sync error", "provider_id", provider.ID, "error", persistErr)
		}
		return err
	}

	client := s.newProviderClient(provider, s.syncClient)
	var response *http.Response
	err = client.Get(ctx, path, nil, &response)
	if err != nil && response == nil {
		persistErr := s.store.setProviderSyncError(ctx, provider.ID, err)
		if persistErr != nil {
			s.logger.Error("persist provider sync error", "provider_id", provider.ID, "error", persistErr)
		}
		return fmt.Errorf("sync provider catalog: %w", err)
	}
	if response == nil {
		syncErr := errors.New("provider sync returned no response")
		persistErr := s.store.setProviderSyncError(ctx, provider.ID, syncErr)
		if persistErr != nil {
			s.logger.Error("persist provider sync error", "provider_id", provider.ID, "error", persistErr)
		}
		return syncErr
	}
	defer response.Body.Close()

	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return fmt.Errorf("read provider catalog response: %w", err)
	}

	if response.StatusCode >= http.StatusBadRequest {
		syncErr := fmt.Errorf("provider returned %s: %s", response.Status, strings.TrimSpace(string(body)))
		persistErr := s.store.setProviderSyncError(ctx, provider.ID, syncErr)
		if persistErr != nil {
			s.logger.Error("persist provider sync error", "provider_id", provider.ID, "error", persistErr)
		}
		return syncErr
	}

	var payload providerModelListResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		syncErr := fmt.Errorf("decode provider models: %w", err)
		persistErr := s.store.setProviderSyncError(ctx, provider.ID, syncErr)
		if persistErr != nil {
			s.logger.Error("persist provider sync error", "provider_id", provider.ID, "error", persistErr)
		}
		return syncErr
	}

	modelIDs := make([]string, 0, len(payload.Data))
	for _, model := range payload.Data {
		if strings.TrimSpace(model.ID) == "" {
			continue
		}
		modelIDs = append(modelIDs, model.ID)
	}

	if err := s.store.syncProviderModels(ctx, provider.ID, modelIDs); err != nil {
		return err
	}

	return nil
}

// syncAllProviders refreshes every enabled provider and returns the result set keyed by provider ID.
func (s *server) syncAllProviders(ctx context.Context) map[int64]string {
	results := make(map[int64]string)
	providers, err := s.store.listProviders(ctx)
	if err != nil {
		s.logger.Error("list providers for sync", "error", err)
		return results
	}

	for _, provider := range providers {
		if err := s.syncProviderCatalog(ctx, provider); err != nil {
			results[provider.ID] = err.Error()
			continue
		}
		results[provider.ID] = ""
	}

	return results
}

// proxyOpenAIRequest forwards OpenAI-compatible traffic to the provider that serves the requested model.
func (s *server) proxyOpenAIRequest(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	auditContext := context.WithoutCancel(r.Context())

	modelID, body, err := ExtractModelHint(r)
	logID := s.createProxyRequestAudit(auditContext, r, modelID, body)
	if err != nil {
		if errors.Is(err, errModelHintMissing) {
			s.writeLoggedProxyError(w, auditContext, logID, startedAt, nil, http.StatusBadRequest, "requests under /v1 must include a model field or target /v1/models/{id}")
			return
		}
		s.writeLoggedProxyError(w, auditContext, logID, startedAt, nil, http.StatusBadRequest, err.Error())
		return
	}

	provider, err := s.store.findProviderForModel(r.Context(), modelID)
	if err != nil {
		if errors.Is(err, errProviderNotFound) {
			s.writeLoggedProxyError(w, auditContext, logID, startedAt, nil, http.StatusNotFound, fmt.Sprintf("no enabled provider serves model %q", modelID))
			return
		}
		s.writeLoggedProxyError(w, auditContext, logID, startedAt, nil, http.StatusInternalServerError, err.Error())
		return
	}

	path, err := resolveProviderPath(provider.BaseURL, r.URL.Path)
	if err != nil {
		s.writeLoggedProxyError(w, auditContext, logID, startedAt, &provider, http.StatusBadGateway, err.Error())
		return
	}

	client := s.newProviderClient(provider, s.proxyClient)
	options := buildProviderRequestOptions(r.Header, r.URL.Query())

	var requestBody any
	if len(body) > 0 {
		requestBody = bytes.NewReader(body)
	}

	var response *http.Response
	err = client.Execute(r.Context(), r.Method, path, requestBody, &response, options...)
	if err != nil && response == nil {
		s.writeLoggedProxyError(w, auditContext, logID, startedAt, &provider, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return
	}
	if response == nil {
		s.writeLoggedProxyError(w, auditContext, logID, startedAt, &provider, http.StatusBadGateway, "upstream request returned no response")
		return
	}

	capture := copyUpstreamResponse(w, response, provider.Name, modelID, s.logger)
	errorText := ""
	if err != nil {
		errorText = err.Error()
	}
	if capture.StreamError != nil {
		if errorText == "" {
			errorText = capture.StreamError.Error()
		} else {
			errorText = fmt.Sprintf("%s; %s", errorText, capture.StreamError.Error())
		}
	}

	s.completeProxyRequestAudit(auditContext, logID, proxyRequestLogUpdate{
		ProviderID:            &provider.ID,
		ProviderName:          provider.Name,
		ResponseStatus:        capture.StatusCode,
		ResponseHeaders:       capture.HeadersJSON,
		ResponseBody:          capture.Body,
		ResponseBodyTruncated: capture.BodyTruncated,
		ErrorText:             errorText,
		DurationMS:            time.Since(startedAt).Milliseconds(),
		CompletedAt:           time.Now(),
	})
}

// toOpenAIModels converts the aggregated route table into the OpenAI models-list response shape.
func toOpenAIModels(routeModels []routeModelView) openAIModelListResponse {
	data := make([]openAIModelResponse, 0, len(routeModels))
	for _, routeModel := range routeModels {
		providers := make([]string, 0, len(routeModel.Providers))
		for _, provider := range routeModel.Providers {
			providers = append(providers, provider.Name)
		}
		slices.Sort(providers)

		data = append(data, openAIModelResponse{
			ID:        routeModel.ID,
			Object:    "model",
			Created:   time.Now().Unix(),
			OwnedBy:   "aggr",
			Providers: providers,
		})
	}

	return openAIModelListResponse{
		Object: "list",
		Data:   data,
	}
}

// copyResponseHeaders copies non-hop-by-hop headers from an upstream response.
func copyResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		if isHopByHopHeader(key) {
			continue
		}
		dst[key] = append([]string(nil), values...)
	}
}

// isHopByHopHeader reports whether a header should be stripped when proxying responses.
func isHopByHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "proxy-connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}
