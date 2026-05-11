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
	"sync"
	"time"

	"github.com/coder/websocket"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const maxProxyBodyBytes = 32 << 20

var errModelHintMissing = errors.New("model hint missing")

const openAIResponsesWebSocketPath = "/v1/responses"

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

// resolvedModelRoute captures the public model requested by the client, the
// upstream model that should actually be sent, and the provider selected to
// handle the request.
type resolvedModelRoute struct {
	// RequestedModelID is the public model name from the inbound client request.
	RequestedModelID string
	// UpstreamModelID is the model name that should be sent to the upstream provider.
	UpstreamModelID string
	// Provider is the enabled provider selected to handle the request.
	Provider providerRecord
}

// responsesWebSocketClientEvent is the top-level client frame shape used by
// OpenAI Responses WebSocket mode.
type responsesWebSocketClientEvent struct {
	// Type identifies the websocket event name, such as `response.create`.
	Type string `json:"type"`
	// Response carries the request payload for `response.create`.
	Response map[string]any `json:"response"`
}

// responsesWebSocketServerEvent is the subset of upstream response event data
// needed to finalize one audited websocket turn.
type responsesWebSocketServerEvent struct {
	// Type identifies the websocket event name.
	Type string `json:"type"`
	// Response carries terminal response metadata for completion, failure, and
	// incomplete events.
	Response map[string]any `json:"response"`
	// Code is the error code emitted by `error` events.
	Code string `json:"code"`
	// Message is the human-readable error text emitted by `error` events.
	Message string `json:"message"`
}

// responsesWebSocketAuditTurn tracks one `response.create` turn flowing across
// a persistent websocket connection so the gateway can record it in the same
// request-audit system used by HTTP proxy calls.
type responsesWebSocketAuditTurn struct {
	// LogID is the inserted audit row for this turn.
	LogID int64
	// StartedAt records when the turn began.
	StartedAt time.Time
	// RequestedModelID is the public model requested by the client.
	RequestedModelID string
	// Route is the resolved provider and upstream model selected for the turn.
	Route resolvedModelRoute
	// SentRequest stores the rewritten upstream request snapshot for auditing.
	SentRequest proxyRequestSentCapture
	// RequestPayload stores the exact rewritten `response.create` frame sent
	// upstream.
	RequestPayload []byte
}

// responsesWebSocketTurnState stores the currently active websocket turn behind
// a mutex because the client-to-upstream and upstream-to-client loops run
// concurrently.
type responsesWebSocketTurnState struct {
	// mu protects turn.
	mu sync.Mutex
	// turn is the active `response.create` request awaiting a terminal event.
	turn *responsesWebSocketAuditTurn
}

// flushingWriter wraps a response writer so long-lived streamed responses flush
// after each copied chunk reaches the caller.
type flushingWriter struct {
	// writer receives the proxied bytes.
	writer io.Writer
	// flusher exposes the HTTP flush primitive for streamed responses.
	flusher http.Flusher
}

// proxyRequestSentCapture stores the exact upstream request that the gateway
// sent while proxying one OpenAI call.
type proxyRequestSentCapture struct {
	// Method is the outbound HTTP verb sent upstream.
	Method string
	// URL is the exact upstream URL that the gateway called.
	URL string
	// HeadersJSON stores the sanitized outbound headers as JSON.
	HeadersJSON string
	// Body stores a capped copy of the outbound request body.
	Body string
	// BodyTruncated reports whether the stored request body preview was shortened.
	BodyTruncated bool
}

// capturingRoundTripper records the outbound request before handing it to the
// real transport, which lets the audit trail show the exact sent payload.
type capturingRoundTripper struct {
	// base is the underlying transport used to send the upstream request.
	base http.RoundTripper
	// capture receives the request snapshot for the audit log.
	capture *proxyRequestSentCapture
}

// RoundTrip records the outbound request snapshot and forwards the request to
// the wrapped transport.
func (roundTripper *capturingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if roundTripper.capture != nil {
		roundTripper.capture.Method = req.Method
		roundTripper.capture.URL = req.URL.String()
		roundTripper.capture.HeadersJSON = headersToAuditJSON(req.Header)
	}

	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, fmt.Errorf("capture upstream request body: %w", err)
		}

		if roundTripper.capture != nil {
			roundTripper.capture.Body, roundTripper.capture.BodyTruncated = truncateAuditBytes(body)
		}

		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
	}

	if roundTripper.base == nil {
		roundTripper.base = http.DefaultTransport
	}

	return roundTripper.base.RoundTrip(req)
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
		modelID := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/v1/models/"))
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

	return strings.TrimSpace(payload.Model), body, nil
}

// resolveModelRoute resolves a public model name into the provider and
// upstream model that should actually be used for proxying.
func (s *server) resolveModelRoute(ctx context.Context, requestedModelID string) (resolvedModelRoute, error) {
	alias, err := s.store.getModelAliasByName(ctx, requestedModelID)
	if err == nil {
		var provider providerRecord
		if alias.TargetProviderID.Valid {
			provider, err = s.store.getRoutableProviderForModel(ctx, alias.TargetProviderID.Int64, alias.TargetModelID)
		} else {
			provider, err = s.store.findProviderForModel(ctx, alias.TargetModelID)
		}
		if err != nil {
			return resolvedModelRoute{}, err
		}

		return resolvedModelRoute{
			RequestedModelID: requestedModelID,
			UpstreamModelID:  alias.TargetModelID,
			Provider:         provider,
		}, nil
	}
	if !errors.Is(err, errModelAliasNotFound) {
		return resolvedModelRoute{}, err
	}

	provider, err := s.store.findProviderForModel(ctx, requestedModelID)
	if err != nil {
		return resolvedModelRoute{}, err
	}

	return resolvedModelRoute{
		RequestedModelID: requestedModelID,
		UpstreamModelID:  requestedModelID,
		Provider:         provider,
	}, nil
}

// rewriteModelRequest prepares the upstream path, query string, and JSON body
// so alias requests are translated into the configured target model.
func rewriteModelRequest(r *http.Request, body []byte, requestedModelID string, upstreamModelID string) (string, url.Values, []byte, error) {
	path := rewriteModelPath(r.URL.Path, requestedModelID, upstreamModelID)
	query := cloneQueryValues(r.URL.Query())
	if query.Has("model") {
		query.Set("model", upstreamModelID)
	}

	rewrittenBody, err := rewriteJSONModelField(body, upstreamModelID)
	if err != nil {
		return "", nil, nil, err
	}

	return path, query, rewrittenBody, nil
}

// rewriteModelPath replaces the first model segment in `/v1/models/{id}` style
// paths so model aliases can target an upstream model name.
func rewriteModelPath(path string, requestedModelID string, upstreamModelID string) string {
	if requestedModelID == upstreamModelID {
		return path
	}

	const modelPathPrefix = "/v1/models/"
	if !strings.HasPrefix(path, modelPathPrefix) {
		return path
	}

	suffix := strings.TrimPrefix(path, modelPathPrefix)
	parts := strings.SplitN(suffix, "/", 2)
	if len(parts) == 0 || parts[0] != requestedModelID {
		return path
	}

	rewritten := modelPathPrefix + upstreamModelID
	if len(parts) == 2 && parts[1] != "" {
		rewritten += "/" + parts[1]
	}

	return rewritten
}

// rewriteJSONModelField replaces the top-level `model` field in a JSON object
// body when the gateway needs to proxy an alias request to another model name.
func rewriteJSONModelField(body []byte, upstreamModelID string) ([]byte, error) {
	if len(body) == 0 {
		return nil, nil
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return append([]byte(nil), body...), nil
	}

	if _, found := payload["model"]; !found {
		return append([]byte(nil), body...), nil
	}

	modelJSON, err := json.Marshal(upstreamModelID)
	if err != nil {
		return nil, fmt.Errorf("encode rewritten model field: %w", err)
	}
	payload["model"] = modelJSON

	rewrittenBody, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode rewritten request body: %w", err)
	}

	return rewrittenBody, nil
}

// cloneQueryValues copies a query map so alias rewrites do not mutate the
// original request URL owned by the server.
func cloneQueryValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, items := range values {
		cloned[key] = append([]string(nil), items...)
	}
	return cloned
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

	requestedModelID, body, err := ExtractModelHint(r)
	logID := s.createProxyRequestAudit(auditContext, r, requestedModelID, body)
	if err != nil {
		if errors.Is(err, errModelHintMissing) {
			s.writeLoggedProxyError(w, auditContext, logID, startedAt, nil, nil, http.StatusBadRequest, "requests under /v1 must include a model field or target /v1/models/{id}")
			return
		}
		s.writeLoggedProxyError(w, auditContext, logID, startedAt, nil, nil, http.StatusBadRequest, err.Error())
		return
	}

	route, err := s.resolveModelRoute(r.Context(), requestedModelID)
	if err != nil {
		if errors.Is(err, errProviderNotFound) {
			s.writeLoggedProxyError(w, auditContext, logID, startedAt, nil, nil, http.StatusNotFound, fmt.Sprintf("no enabled provider serves model %q", requestedModelID))
			return
		}
		s.writeLoggedProxyError(w, auditContext, logID, startedAt, nil, nil, http.StatusInternalServerError, err.Error())
		return
	}

	upstreamPath, upstreamQuery, upstreamBody, err := rewriteModelRequest(r, body, route.RequestedModelID, route.UpstreamModelID)
	if err != nil {
		s.writeLoggedProxyError(w, auditContext, logID, startedAt, &route.Provider, nil, http.StatusBadRequest, err.Error())
		return
	}

	path, err := resolveProviderPath(route.Provider.BaseURL, upstreamPath)
	if err != nil {
		s.writeLoggedProxyError(w, auditContext, logID, startedAt, &route.Provider, nil, http.StatusBadGateway, err.Error())
		return
	}

	sentRequest := proxyRequestSentCapture{}
	proxyClient := *s.proxyClient
	proxyClient.Transport = &capturingRoundTripper{
		base:    proxyClient.Transport,
		capture: &sentRequest,
	}
	client := s.newProviderClient(route.Provider, &proxyClient)
	options := buildProviderRequestOptions(r.Header, upstreamQuery)

	var requestBody any
	if len(upstreamBody) > 0 {
		requestBody = bytes.NewReader(upstreamBody)
	}

	var response *http.Response
	err = client.Execute(r.Context(), r.Method, path, requestBody, &response, options...)
	if err != nil && response == nil {
		s.writeLoggedProxyError(w, auditContext, logID, startedAt, &route.Provider, &sentRequest, http.StatusBadGateway, fmt.Sprintf("upstream request failed: %v", err))
		return
	}
	if response == nil {
		s.writeLoggedProxyError(w, auditContext, logID, startedAt, &route.Provider, &sentRequest, http.StatusBadGateway, "upstream request returned no response")
		return
	}

	capture := copyUpstreamResponse(w, response, route.Provider.Name, route.RequestedModelID, s.logger)
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
		SentMethod:            sentRequest.Method,
		SentURL:               sentRequest.URL,
		SentHeaders:           sentRequest.HeadersJSON,
		SentBody:              sentRequest.Body,
		SentBodyTruncated:     sentRequest.BodyTruncated,
		ProviderID:            &route.Provider.ID,
		ProviderName:          route.Provider.Name,
		ResponseStatus:        capture.StatusCode,
		ResponseHeaders:       capture.HeadersJSON,
		ResponseBody:          capture.Body,
		ResponseBodyTruncated: capture.BodyTruncated,
		ErrorText:             errorText,
		DurationMS:            time.Since(startedAt).Milliseconds(),
		CompletedAt:           time.Now(),
	})
}

// shouldHandleOpenAIResponsesWebSocket reports whether the incoming request is
// a WebSocket upgrade targeting the Responses API websocket-mode endpoint.
func shouldHandleOpenAIResponsesWebSocket(r *http.Request) bool {
	if r.URL.Path != openAIResponsesWebSocketPath {
		return false
	}

	return headerContainsToken(r.Header, "Connection", "Upgrade") &&
		headerContainsToken(r.Header, "Upgrade", "websocket")
}

// proxyOpenAIWebSocket accepts a client websocket for `/v1/responses`, routes
// it to the provider selected by the first `response.create` turn, and proxies
// subsequent frames while recording per-turn audit rows.
func (s *server) proxyOpenAIWebSocket(w http.ResponseWriter, r *http.Request) {
	clientConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	})
	if err != nil {
		s.logger.Warn("accept responses websocket", "error", err)
		return
	}
	defer clientConn.CloseNow()

	ctx := context.Background()
	turnState := &responsesWebSocketTurnState{}

	clientMessageType, clientPayload, err := clientConn.Read(ctx)
	if err != nil {
		s.logger.Warn("read first websocket frame", "error", err)
		_ = clientConn.Close(websocket.StatusPolicyViolation, "failed to read initial websocket message")
		return
	}

	route, initialUpstreamPayload, turn, err := s.prepareResponsesWebSocketTurn(r, clientPayload)
	if err != nil {
		s.writeResponsesWebSocketGatewayError(ctx, clientConn, nil, err)
		return
	}
	turnState.set(turn)

	upstreamURL, err := ResolveProviderURL(route.Provider.BaseURL, openAIResponsesWebSocketPath, r.URL.RawQuery)
	if err != nil {
		s.writeResponsesWebSocketGatewayError(ctx, clientConn, turnState.take(), err)
		return
	}

	upstreamHeaders := cloneHeadersForWebSocket(r.Header)
	upstreamHeaders.Del("Authorization")
	upstreamHeaders.Del("Host")
	upstreamHeaders.Del("Cookie")
	upstreamHeaders.Del("Origin")
	upstreamHeaders.Set("User-Agent", route.Provider.UserAgent)

	upstreamConn, response, err := websocket.Dial(ctx, toWebSocketURL(upstreamURL), &websocket.DialOptions{
		HTTPClient: s.proxyClient,
		HTTPHeader: upstreamHeaders,
	})
	if err != nil {
		status := http.StatusBadGateway
		if response != nil && response.StatusCode > 0 {
			status = response.StatusCode
		}
		s.finalizeResponsesWebSocketTurn(turnState.take(), status, nil, fmt.Sprintf("upstream websocket dial failed: %v", err))
		_ = writeResponsesWebSocketJSON(ctx, clientConn, map[string]any{
			"type":    "error",
			"code":    "upstream_websocket_dial_failed",
			"message": fmt.Sprintf("upstream websocket dial failed: %v", err),
			"param":   "",
		})
		_ = clientConn.Close(websocket.StatusPolicyViolation, "upstream websocket dial failed")
		return
	}
	defer upstreamConn.CloseNow()

	currentTurn := turnState.current()
	currentTurn.SentRequest = proxyRequestSentCapture{
		Method:      http.MethodGet,
		URL:         upstreamURL,
		HeadersJSON: headersToAuditJSON(upstreamHeaders),
		Body:        string(initialUpstreamPayload),
	}

	if err := upstreamConn.Write(ctx, clientMessageType, initialUpstreamPayload); err != nil {
		s.finalizeResponsesWebSocketTurn(turnState.take(), http.StatusBadGateway, nil, fmt.Sprintf("write initial websocket frame upstream: %v", err))
		_ = writeResponsesWebSocketJSON(ctx, clientConn, map[string]any{
			"type":    "error",
			"code":    "upstream_write_failed",
			"message": fmt.Sprintf("write initial websocket frame upstream: %v", err),
			"param":   "",
		})
		_ = clientConn.Close(websocket.StatusPolicyViolation, "upstream write failed")
		return
	}

	upstreamDone := make(chan error, 1)
	go func() {
		upstreamDone <- s.proxyResponsesWebSocketUpstreamToClient(ctx, clientConn, upstreamConn, turnState)
	}()

	for {
		messageType, payload, readErr := clientConn.Read(ctx)
		if readErr != nil {
			_ = upstreamConn.Close(websocket.StatusNormalClosure, "")
			upstreamErr := <-upstreamDone
			if pendingTurn := turnState.take(); pendingTurn != nil {
				s.finalizeResponsesWebSocketTurn(pendingTurn, http.StatusBadGateway, nil, "client websocket closed before the response finished")
			}
			if upstreamErr != nil {
				s.logger.Warn("proxy responses websocket upstream loop", "error", upstreamErr)
			}
			return
		}

		nextRoute, nextPayload, nextTurn, rewriteErr := s.prepareResponsesWebSocketTurn(r, payload)
		if rewriteErr != nil {
			s.writeResponsesWebSocketGatewayError(ctx, clientConn, nil, rewriteErr)
			continue
		}
		if nextTurn != nil {
			if route.Provider.ID != nextRoute.Provider.ID {
				s.finalizeResponsesWebSocketTurn(nextTurn, http.StatusBadRequest, nil, "websocket mode cannot switch providers after the connection is established")
				_ = writeResponsesWebSocketJSON(ctx, clientConn, map[string]any{
					"type":    "error",
					"code":    "provider_switch_not_supported",
					"message": "websocket mode cannot switch providers after the connection is established",
					"param":   "response.model",
				})
				continue
			}

			nextTurn.SentRequest = proxyRequestSentCapture{
				Method:      http.MethodGet,
				URL:         upstreamURL,
				HeadersJSON: headersToAuditJSON(upstreamHeaders),
				Body:        string(nextPayload),
			}
			turnState.set(nextTurn)
			payload = nextPayload
		}

		if err := upstreamConn.Write(ctx, messageType, payload); err != nil {
			if pendingTurn := turnState.take(); pendingTurn != nil {
				s.finalizeResponsesWebSocketTurn(pendingTurn, http.StatusBadGateway, nil, fmt.Sprintf("write websocket frame upstream: %v", err))
			}
			_ = writeResponsesWebSocketJSON(ctx, clientConn, map[string]any{
				"type":    "error",
				"code":    "upstream_write_failed",
				"message": fmt.Sprintf("write websocket frame upstream: %v", err),
				"param":   "",
			})
			_ = upstreamConn.Close(websocket.StatusPolicyViolation, "upstream write failed")
			_ = clientConn.Close(websocket.StatusPolicyViolation, "upstream write failed")
			<-upstreamDone
			return
		}
	}
}

// prepareResponsesWebSocketTurn validates one client websocket frame, resolves
// routing for `response.create` events, rewrites alias models, and inserts a
// request-audit row for that turn.
func (s *server) prepareResponsesWebSocketTurn(r *http.Request, payload []byte) (resolvedModelRoute, []byte, *responsesWebSocketAuditTurn, error) {
	var event responsesWebSocketClientEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return resolvedModelRoute{}, nil, nil, fmt.Errorf("decode websocket payload: %w", err)
	}

	if event.Type != "response.create" {
		return resolvedModelRoute{}, payload, nil, nil
	}

	requestedModelID, err := extractResponsesWebSocketModelHint(r, event)
	if err != nil {
		return resolvedModelRoute{}, nil, nil, err
	}

	route, err := s.resolveModelRoute(r.Context(), requestedModelID)
	if err != nil {
		if errors.Is(err, errProviderNotFound) {
			return resolvedModelRoute{}, nil, nil, fmt.Errorf("no enabled provider serves model %q", requestedModelID)
		}
		return resolvedModelRoute{}, nil, nil, err
	}

	rewrittenPayload, err := rewriteResponsesWebSocketEventModel(payload, route.UpstreamModelID)
	if err != nil {
		return resolvedModelRoute{}, nil, nil, err
	}

	auditContext := context.Background()
	logID := s.createProxyRequestAudit(auditContext, &http.Request{
		Method: http.MethodGet,
		URL: &url.URL{
			Path:     r.URL.Path,
			RawQuery: r.URL.RawQuery,
		},
		Header: r.Header.Clone(),
	}, requestedModelID, payload)

	return route, rewrittenPayload, &responsesWebSocketAuditTurn{
		LogID:            logID,
		StartedAt:        time.Now(),
		RequestedModelID: requestedModelID,
		Route:            route,
		RequestPayload:   rewrittenPayload,
	}, nil
}

// proxyResponsesWebSocketUpstreamToClient copies upstream websocket frames to
// the client and finalizes the active audit turn when a terminal response event
// arrives.
func (s *server) proxyResponsesWebSocketUpstreamToClient(ctx context.Context, clientConn *websocket.Conn, upstreamConn *websocket.Conn, turnState *responsesWebSocketTurnState) error {
	for {
		messageType, payload, err := upstreamConn.Read(ctx)
		if err != nil {
			return err
		}

		if turn := turnState.current(); turn != nil {
			if finalized := s.tryFinalizeResponsesWebSocketTurn(turn, payload); finalized {
				turnState.clearCurrent(turn.LogID)
			}
		}

		if err := clientConn.Write(ctx, messageType, payload); err != nil {
			return err
		}
	}
}

// tryFinalizeResponsesWebSocketTurn inspects one upstream websocket payload
// and finalizes the current turn when it reaches a terminal response event.
func (s *server) tryFinalizeResponsesWebSocketTurn(turn *responsesWebSocketAuditTurn, payload []byte) bool {
	var event responsesWebSocketServerEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return false
	}

	switch event.Type {
	case "response.completed":
		s.finalizeResponsesWebSocketTurn(turn, http.StatusOK, payload, "")
		return true
	case "response.failed":
		s.finalizeResponsesWebSocketTurn(turn, http.StatusBadGateway, payload, extractResponsesWebSocketResponseError(event.Response))
		return true
	case "response.incomplete":
		s.finalizeResponsesWebSocketTurn(turn, http.StatusBadGateway, payload, "response incomplete")
		return true
	case "error":
		status := http.StatusBadGateway
		if turn.LogID == 0 {
			status = http.StatusBadRequest
		}
		errorText := strings.TrimSpace(event.Message)
		if errorText == "" {
			errorText = strings.TrimSpace(event.Code)
		}
		s.finalizeResponsesWebSocketTurn(turn, status, payload, errorText)
		return true
	default:
		return false
	}
}

// finalizeResponsesWebSocketTurn persists the completed audit record for one
// websocket-mode Responses turn.
func (s *server) finalizeResponsesWebSocketTurn(turn *responsesWebSocketAuditTurn, responseStatus int, responsePayload []byte, errorText string) {
	if turn == nil {
		return
	}

	responseHeaders := http.Header{}
	responseHeaders.Set("Content-Type", "application/json")
	responseHeaders.Set("X-Aggr-Provider", turn.Route.Provider.Name)
	if turn.RequestedModelID != "" {
		responseHeaders.Set("X-Aggr-Model", turn.RequestedModelID)
	}

	responseBody, responseBodyTruncated := truncateAuditBytes(responsePayload)
	s.completeProxyRequestAudit(context.Background(), turn.LogID, proxyRequestLogUpdate{
		SentMethod:            turn.SentRequest.Method,
		SentURL:               turn.SentRequest.URL,
		SentHeaders:           turn.SentRequest.HeadersJSON,
		SentBody:              turn.SentRequest.Body,
		SentBodyTruncated:     turn.SentRequest.BodyTruncated,
		ProviderID:            &turn.Route.Provider.ID,
		ProviderName:          turn.Route.Provider.Name,
		ResponseStatus:        responseStatus,
		ResponseHeaders:       headersToAuditJSON(responseHeaders),
		ResponseBody:          responseBody,
		ResponseBodyTruncated: responseBodyTruncated,
		ErrorText:             strings.TrimSpace(errorText),
		DurationMS:            time.Since(turn.StartedAt).Milliseconds(),
		CompletedAt:           time.Now(),
	})
}

// writeResponsesWebSocketGatewayError returns a gateway-generated websocket
// error frame to the client and finalizes the current turn when one exists.
func (s *server) writeResponsesWebSocketGatewayError(ctx context.Context, clientConn *websocket.Conn, turn *responsesWebSocketAuditTurn, err error) {
	message := strings.TrimSpace(err.Error())
	if turn != nil {
		s.finalizeResponsesWebSocketTurn(turn, http.StatusBadRequest, nil, message)
	}

	_ = writeResponsesWebSocketJSON(ctx, clientConn, map[string]any{
		"type":    "error",
		"code":    "gateway_error",
		"message": message,
		"param":   "",
	})
}

// extractResponsesWebSocketModelHint reads the requested public model name from
// a websocket-mode `response.create` event or the `model` query parameter.
func extractResponsesWebSocketModelHint(r *http.Request, event responsesWebSocketClientEvent) (string, error) {
	if event.Type != "response.create" {
		return "", errModelHintMissing
	}

	if modelValue, ok := event.Response["model"]; ok {
		if modelID, ok := modelValue.(string); ok && strings.TrimSpace(modelID) != "" {
			return strings.TrimSpace(modelID), nil
		}
	}

	if modelID := strings.TrimSpace(r.URL.Query().Get("model")); modelID != "" {
		return modelID, nil
	}

	return "", errors.New("responses websocket mode requires a model in the response.create payload or the model query parameter")
}

// rewriteResponsesWebSocketEventModel rewrites the nested `response.model`
// field in a websocket-mode client event.
func rewriteResponsesWebSocketEventModel(payload []byte, upstreamModelID string) ([]byte, error) {
	var event map[string]any
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, fmt.Errorf("decode websocket event for model rewrite: %w", err)
	}

	responsePayload, ok := event["response"].(map[string]any)
	if !ok {
		return append([]byte(nil), payload...), nil
	}

	if _, found := responsePayload["model"]; found {
		responsePayload["model"] = upstreamModelID
	}

	rewritten, err := json.Marshal(event)
	if err != nil {
		return nil, fmt.Errorf("encode rewritten websocket event: %w", err)
	}

	return rewritten, nil
}

// extractResponsesWebSocketResponseError reads a terminal error message from a
// Responses websocket response object when one is present.
func extractResponsesWebSocketResponseError(response map[string]any) string {
	errorValue, ok := response["error"].(map[string]any)
	if !ok {
		return ""
	}

	if message, ok := errorValue["message"].(string); ok {
		return strings.TrimSpace(message)
	}

	if code, ok := errorValue["code"].(string); ok {
		return strings.TrimSpace(code)
	}

	return ""
}

// cloneHeadersForWebSocket copies request headers so websocket dials can pass
// through supported values without mutating the inbound request.
func cloneHeadersForWebSocket(headers http.Header) http.Header {
	cloned := make(http.Header, len(headers))
	for key, values := range headers {
		cloned[key] = append([]string(nil), values...)
	}
	return cloned
}

// toWebSocketURL converts an HTTP(S) endpoint into its websocket equivalent.
func toWebSocketURL(raw string) string {
	if strings.HasPrefix(raw, "https://") {
		return "wss://" + strings.TrimPrefix(raw, "https://")
	}
	if strings.HasPrefix(raw, "http://") {
		return "ws://" + strings.TrimPrefix(raw, "http://")
	}
	return raw
}

// writeResponsesWebSocketJSON sends one JSON websocket frame to the caller.
func writeResponsesWebSocketJSON(ctx context.Context, conn *websocket.Conn, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode websocket payload: %w", err)
	}

	return conn.Write(ctx, websocket.MessageText, body)
}

// set replaces the currently active websocket turn.
func (state *responsesWebSocketTurnState) set(turn *responsesWebSocketAuditTurn) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.turn = turn
}

// current returns the currently active websocket turn without removing it.
func (state *responsesWebSocketTurnState) current() *responsesWebSocketAuditTurn {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.turn
}

// take removes and returns the currently active websocket turn.
func (state *responsesWebSocketTurnState) take() *responsesWebSocketAuditTurn {
	state.mu.Lock()
	defer state.mu.Unlock()
	turn := state.turn
	state.turn = nil
	return turn
}

// clearCurrent removes the active websocket turn when it matches the provided
// audit log ID, which avoids clearing a newly-started turn due to a race with
// the upstream reader loop.
func (state *responsesWebSocketTurnState) clearCurrent(logID int64) {
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.turn != nil && state.turn.LogID == logID {
		state.turn = nil
	}
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

// headerContainsToken reports whether a comma-delimited header contains the
// provided token, ignoring ASCII case and optional surrounding whitespace.
func headerContainsToken(headers http.Header, name string, token string) bool {
	for _, rawValue := range headers.Values(name) {
		for _, part := range strings.Split(rawValue, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}

	return false
}
