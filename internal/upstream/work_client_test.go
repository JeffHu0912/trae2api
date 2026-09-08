package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"trae2api/internal/auth"
)

func mockAuth() *auth.Auth {
	a := &auth.Auth{
		AccessToken: "mock-jwt-token-123456",
		UID:         "4122512616609817",
		DeviceID:    "2355572504545628",
		MachineID:   "e35d7904442cfebda524b05dfaad9df295cd5bd245fb5495ee25e5b490772137",
		ExpiresAt:   time.Now().Add(1 * time.Hour).Unix(),
	}
	return a
}

func TestWorkClient_ConfigAndHeaders(t *testing.T) {
	client := NewWorkClient(WorkClientConfig{
		Host:        "https://api5-normal.mchost.guru",
		BridgeURL:   "http://127.0.0.1:7865",
		BridgeToken: "test-token",
		Mode:        WorkModeNative,
	})

	if client.cfg.Host != "https://api5-normal.mchost.guru" {
		t.Fatalf("unexpected host: %s", client.cfg.Host)
	}
	if client.cfg.Mode != WorkModeNative {
		t.Fatalf("unexpected mode: %s", client.cfg.Mode)
	}

	a := mockAuth()
	req, err := http.NewRequest(http.MethodPost, "https://api5-normal.mchost.guru/api/agent/v3/custom_route", nil)
	if err != nil {
		t.Fatalf("failed to create req: %v", err)
	}

	ApplyWorkHeaders(req, a, true, false)

	if req.Header.Get("Authorization") != "Cloud-IDE-JWT mock-jwt-token-123456" {
		t.Errorf("Authorization header incorrect: %s", req.Header.Get("Authorization"))
	}
	if req.Header.Get("X-App-Id") != WorkAppID {
		t.Errorf("X-App-Id header incorrect: %s", req.Header.Get("X-App-Id"))
	}
	if req.Header.Get("X-Uid") != a.UID {
		t.Errorf("X-Uid header incorrect: %s", req.Header.Get("X-Uid"))
	}
	if req.Header.Get("X-Device-Id") != a.DeviceID {
		t.Errorf("X-Device-Id header incorrect: %s", req.Header.Get("X-Device-Id"))
	}
	if req.Header.Get("X-Machine-Id") != a.MachineID {
		t.Errorf("X-Machine-Id header incorrect: %s", req.Header.Get("X-Machine-Id"))
	}
	if req.Header.Get("Request-Traffic-Type") != "prod" {
		t.Errorf("Request-Traffic-Type header incorrect: %s", req.Header.Get("Request-Traffic-Type"))
	}
}

func TestWorkClient_ExtractPrompt(t *testing.T) {
	messages := []any{
		map[string]any{"role": "system", "content": "system prompt"},
		map[string]any{"role": "user", "content": "first prompt"},
		map[string]any{"role": "assistant", "content": "reply"},
		map[string]any{"role": "user", "content": "latest work prompt"},
	}

	prompt := ExtractLastUserPrompt(messages)
	if prompt != "latest work prompt" {
		t.Fatalf("expected 'latest work prompt', got: %s", prompt)
	}
}

func TestWorkClient_BuildNativePayload(t *testing.T) {
	a := mockAuth()
	raw := BuildNativeTaskPayload(a, "DeepSeek-V4-Flash-Official", "test prompt", "conv-1", "sess-1")

	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if obj["conversation_id"] != "conv-1" {
		t.Errorf("expected conv-1, got %v", obj["conversation_id"])
	}
	if obj["session_id"] != "sess-1" {
		t.Errorf("expected sess-1, got %v", obj["session_id"])
	}
	if obj["model_name"] != "DeepSeek-V4-Flash-Official__dev" {
		t.Errorf("expected DeepSeek-V4-Flash-Official__dev, got %v", obj["model_name"])
	}
}

func TestWorkClient_StreamWorkToOpenAI(t *testing.T) {
	mockSSE := `event: plan_item
data: {"event":"plan_item","payload":{"thought":"","reasoning_content":"正在思考方案...","tool_call_info":{"params":{"summary":""}}}}

event: output
data: {"event":"output","payload":{"choices":[{"text":"这是最终的"}]}}

event: output
data: {"event":"output","payload":{"choices":[{"text":"这是最终的测试答案"}]}}

event: token_usage
data: {"event":"token_usage","payload":{"prompt_tokens":10,"completion_tokens":25,"total_tokens":35}}

event: done
data: {"event":"done","payload":{"last_assistant_response":"[\"这是最终的测试答案\"]"}}

`
	w := httptest.NewRecorder()
	r := strings.NewReader(mockSSE)

	err := StreamWorkToOpenAI(w, r, "DeepSeek-V4-Flash-Official", "cmpl-test-123")
	if err != nil {
		t.Fatalf("StreamWorkToOpenAI returned error: %v", err)
	}

	body := w.Body.String()
	if !strings.Contains(body, "正在思考方案...") {
		t.Errorf("missing reasoning_content in stream output: %s", body)
	}
	if !strings.Contains(body, "测试答案") {
		t.Errorf("missing text delta in stream output: %s", body)
	}
	if !strings.Contains(body, "[DONE]") {
		t.Errorf("missing [DONE] in stream output: %s", body)
	}
}

func TestWorkClient_AggregateWork(t *testing.T) {
	mockSSE := `event: plan_item
data: {"event":"plan_item","payload":{"thought":"初步规划","tool_call_info":{"params":{"summary":""}}}}

event: output
data: {"event":"output","payload":{"choices":[{"text":"Hello "}]}}

event: output
data: {"event":"output","payload":{"choices":[{"text":"World!"}]}}

event: done
data: {"event":"done","payload":{}}
`
	r := strings.NewReader(mockSSE)
	resp, err := AggregateWork(r, "DeepSeek-V4-Flash-Official", "cmpl-agg-123")
	if err != nil {
		t.Fatalf("AggregateWork failed: %v", err)
	}

	choices, ok := resp["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatalf("invalid choices: %v", resp)
	}
	firstChoice := choices[0].(map[string]any)
	msg := firstChoice["message"].(map[string]any)
	content := msg["content"].(string)

	if !strings.Contains(content, "初步规划") || !strings.Contains(content, "Hello World!") {
		t.Errorf("unexpected content: %s", content)
	}
}

func TestWorkClient_ProbeCreditsMock(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"billing_mode\":\"credits\",\"cn_credits_remain_info\":{\"ide_credits\":0,\"work_credits\":1987.0284}}\n\n"))
	}))
	defer ts.Close()

	client := NewWorkClient(WorkClientConfig{
		Host: ts.URL,
		Mode: WorkModeNative,
	})

	a := mockAuth()
	snap, err := client.ProbeCredits(context.Background(), a)
	if err != nil {
		t.Fatalf("ProbeCredits failed: %v", err)
	}

	if snap.IdeCredits != 0 {
		t.Errorf("expected 0 ide_credits, got %f", snap.IdeCredits)
	}
	if snap.WorkCredits != 1987.0284 {
		t.Errorf("expected 1987.0284 work_credits, got %f", snap.WorkCredits)
	}
}
