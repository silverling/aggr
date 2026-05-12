package server

import "testing"

// TestExtractRequestTokenUsageFromJSONResponse verifies that plain JSON audit
// bodies still produce the expected token-usage summary when the response was
// recorded as `application/json`.
func TestExtractRequestTokenUsageFromJSONResponse(t *testing.T) {
	t.Parallel()

	headers := `{"Content-Type":["application/json; charset=utf-8"]}`
	body := `{"usage":{"prompt_tokens":120,"completion_tokens":30,"prompt_tokens_details":{"cached_tokens":100}}}`

	usage, ok := extractRequestTokenUsage(headers, body)
	if !ok {
		t.Fatal("expected JSON usage to be detected")
	}

	if usage.InputTokens != 120 {
		t.Fatalf("expected 120 input tokens, got %d", usage.InputTokens)
	}
	if usage.CachedInputTokens != 100 {
		t.Fatalf("expected 100 cached input tokens, got %d", usage.CachedInputTokens)
	}
	if usage.NonCachedInputTokens != 20 {
		t.Fatalf("expected 20 non-cached input tokens, got %d", usage.NonCachedInputTokens)
	}
	if usage.OutputTokens != 30 {
		t.Fatalf("expected 30 output tokens, got %d", usage.OutputTokens)
	}
}

// TestExtractRequestTokenUsageFromEventStreamResponse verifies that SSE audit
// bodies use the last usage-bearing `data:` event before `[DONE]`.
func TestExtractRequestTokenUsageFromEventStreamResponse(t *testing.T) {
	t.Parallel()

	headers := `{"content-type":["text/event-stream; charset=utf-8"]}`
	body := "data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n" +
		"data: {\"id\":\"chunk-2\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":11540,\"completion_tokens\":32,\"total_tokens\":11572,\"prompt_tokens_details\":{\"cached_tokens\":11520},\"completion_tokens_details\":{\"reasoning_tokens\":15},\"prompt_cache_hit_tokens\":11520,\"prompt_cache_miss_tokens\":20}}\n\n" +
		"data: [DONE]\n\n"

	usage, ok := extractRequestTokenUsage(headers, body)
	if !ok {
		t.Fatal("expected event-stream usage to be detected")
	}

	if usage.InputTokens != 11540 {
		t.Fatalf("expected 11540 input tokens, got %d", usage.InputTokens)
	}
	if usage.CachedInputTokens != 11520 {
		t.Fatalf("expected 11520 cached input tokens, got %d", usage.CachedInputTokens)
	}
	if usage.NonCachedInputTokens != 20 {
		t.Fatalf("expected 20 non-cached input tokens, got %d", usage.NonCachedInputTokens)
	}
	if usage.OutputTokens != 32 {
		t.Fatalf("expected 32 output tokens, got %d", usage.OutputTokens)
	}
}

// TestExtractRequestTokenUsageFromWebSocketResponse verifies that terminal
// websocket response payloads continue to read nested `response.usage` data.
func TestExtractRequestTokenUsageFromWebSocketResponse(t *testing.T) {
	t.Parallel()

	headers := `{"Content-Type":["application/json"]}`
	body := `{"type":"response.completed","response":{"usage":{"input_tokens":400,"output_tokens":60,"input_tokens_details":{"cached_tokens":250}}}}`

	usage, ok := extractRequestTokenUsage(headers, body)
	if !ok {
		t.Fatal("expected websocket JSON usage to be detected")
	}

	if usage.InputTokens != 400 {
		t.Fatalf("expected 400 input tokens, got %d", usage.InputTokens)
	}
	if usage.CachedInputTokens != 250 {
		t.Fatalf("expected 250 cached input tokens, got %d", usage.CachedInputTokens)
	}
	if usage.NonCachedInputTokens != 150 {
		t.Fatalf("expected 150 non-cached input tokens, got %d", usage.NonCachedInputTokens)
	}
	if usage.OutputTokens != 60 {
		t.Fatalf("expected 60 output tokens, got %d", usage.OutputTokens)
	}
}

// TestExtractRequestTokenUsageIgnoresEventStreamPayloadWithoutUsage verifies
// that an SSE transcript without a usage-bearing chunk does not fabricate token
// counts.
func TestExtractRequestTokenUsageIgnoresEventStreamPayloadWithoutUsage(t *testing.T) {
	t.Parallel()

	headers := `{"Content-Type":["text/event-stream"]}`
	body := "data: {\"id\":\"chunk-1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"},\"finish_reason\":null}]}\n\n" +
		"data: [DONE]\n\n"

	_, ok := extractRequestTokenUsage(headers, body)
	if ok {
		t.Fatal("expected event-stream payload without usage to be ignored")
	}
}
