package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// HeaderContainsToken reports whether a comma-delimited header contains the
// provided token, ignoring ASCII case and optional surrounding whitespace.
func HeaderContainsToken(headers http.Header, name string, token string) bool {
	for _, rawValue := range headers.Values(name) {
		for _, part := range strings.Split(rawValue, ",") {
			if strings.EqualFold(strings.TrimSpace(part), token) {
				return true
			}
		}
	}

	return false
}

// IsHopByHopHeader reports whether a header should be stripped when proxying responses.
func IsHopByHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "proxy-connection", "keep-alive", "proxy-authenticate", "proxy-authorization", "te", "trailer", "transfer-encoding", "upgrade":
		return true
	default:
		return false
	}
}

// CopyResponseHeaders copies non-hop-by-hop headers from an upstream response.
func CopyResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		if IsHopByHopHeader(key) {
			continue
		}
		dst[key] = append([]string(nil), values...)
	}
}

// ToWebSocketURL converts an HTTP(S) endpoint into its websocket equivalent.
func ToWebSocketURL(raw string) string {
	if after, ok := strings.CutPrefix(raw, "https://"); ok {
		return "wss://" + after
	}
	if after, ok := strings.CutPrefix(raw, "http://"); ok {
		return "ws://" + after
	}
	return raw
}

// EncodeJSONPayload marshals a JSON response body so callers can both write it
// to the client and persist the exact bytes in the audit log.
func EncodeJSONPayload(payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode json payload: %w", err)
	}
	return body, nil
}
