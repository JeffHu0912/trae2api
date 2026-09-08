package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"trae2api/internal/auth"
	"trae2api/internal/pool"
	"trae2api/internal/upstream"
)

// 模拟 SOLO SSE 响应（glm-5.2 回答"你好"）。
const soloSSE = "event:metadata\ndata:{\"model\":\"\",\"session_id\":\"s1\"}\n\n" +
	"event:output\ndata:{\"response\":\"你好\",\"reasoning_content\":\"想一下\",\"tool_calls\":null}\n\n" +
	"event:token_usage\ndata:{\"prompt_tokens\":5,\"completion_tokens\":2,\"total_tokens\":7}\n\n" +
	"event:done\ndata:{\"finish_reason\":\"stop\"}\n\n"

// newFakeUpstream 返回 ChatStream 走 fake 的 upstream.Client。
func newFakeUpstream(t *testing.T, behavior func(auth string) (status int, body string, isStream bool)) *upstream.Client {
	t.Helper()
	return &upstream.Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			authz := r.Header.Get("Authorization")
			status, body, isStream := behavior(authz)
			ct := "application/json"
			if isStream {
				ct = "text/event-stream"
			}
			return &http.Response{
				StatusCode: status,
				Header:     http.Header{"Content-Type": []string{ct}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}, nil
		})},
		AgentHost: "https://fake.example",
		UgHost:    "https://fake.example",
		OAuthHost: "https://fake.example",
		ClientID:  upstream.ClientID,
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func testPoolWith(auths ...*auth.Auth) *pool.Pool {
	p := pool.New("")
	for _, a := range auths {
		p.Add(a)
		p.SetCredits(a.UID, 1000)
	}
	return p
}

func TestChatNonStreamAggregates(t *testing.T) {
	up := newFakeUpstream(t, func(authz string) (int, string, bool) {
		if authz != "Cloud-IDE-JWT at1" {
			t.Errorf("auth=%q", authz)
		}
		return 200, soloSSE, true
	})
	h := NewHandler(Config{
		Pool:     testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at1", ExpiresAt: 9999999999}),
		Upstream: up,
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("resp not json: %v body=%s", err, rec.Body)
	}
	if resp["object"] != "chat.completion" {
		t.Errorf("object=%v", resp["object"])
	}
	msg := resp["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
	if msg["content"] != "你好" {
		t.Errorf("content=%q", msg["content"])
	}
	if msg["reasoning_content"] != "想一下" {
		t.Errorf("reasoning=%q", msg["reasoning_content"])
	}
}

func TestChatStreamPassthrough(t *testing.T) {
	up := newFakeUpstream(t, func(authz string) (int, string, bool) {
		return 200, soloSSE, true
	})
	h := NewHandler(Config{
		Pool:     testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at1", ExpiresAt: 9999999999}),
		Upstream: up,
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"glm-5.2","stream":true,"messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("ct=%q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "你好") || !strings.Contains(body, "data: [DONE]") {
		t.Errorf("body=%q", body)
	}
}

func TestChatRotatesOnPlanLimit(t *testing.T) {
	calls := map[string]int{}
	up := newFakeUpstream(t, func(authz string) (int, string, bool) {
		calls[authz]++
		if authz == "Cloud-IDE-JWT at-bad" {
			return 200, "event:error\ndata:{\"code\":1005,\"message\":\"plan limit\",\"extra\":{\"plan\":2}}\n\n", true
		}
		return 200, soloSSE, true
	})
	p := testPoolWith(
		&auth.Auth{UID: "bad", AccessToken: "at-bad", ExpiresAt: 9999999999},
		&auth.Auth{UID: "good", AccessToken: "at-good", ExpiresAt: 9999999999},
	)
	p.SetCredits("bad", 2000)
	p.SetCredits("good", 1000)
	h := NewHandler(Config{Pool: p, Upstream: up})
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"glm-5.2","messages":[]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if calls["Cloud-IDE-JWT at-bad"] != 1 || calls["Cloud-IDE-JWT at-good"] != 1 {
		t.Errorf("calls=%v", calls)
	}
}

// TestChatStreamCooldownOnStreamError 流式请求中上游 event:error（如 5xx/参数错误）
// 应触发 NoteError 冷却（而非仅透传不冷却）。
func TestChatStreamCooldownOnStreamError(t *testing.T) {
	up := newFakeUpstream(t, func(authz string) (int, string, bool) {
		return 200, "event:error\ndata:{\"code\":5001,\"message\":\"upstream broke\"}\n\n", true
	})
	p := testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at1", ExpiresAt: 9999999999})
	h := NewHandler(Config{Pool: p, Upstream: up})
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"glm-5.2","stream":true,"messages":[]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// 流内错误不应中断整个请求的 SSE 输出（仍有 event:error + [DONE]）。
	body := rec.Body.String()
	if !strings.Contains(body, "solo error") || !strings.Contains(body, "data: [DONE]") {
		t.Errorf("body=%q want error event + [DONE]", body)
	}
	st, _ := p.Status("u1")
	if st.ErrCount == 0 {
		t.Errorf("stream error should bump errCount: %+v", st)
	}
}

func TestChatAllUnavailableReturns503(t *testing.T) {
	up := newFakeUpstream(t, func(authz string) (int, string, bool) {
		return 429, `rate limited`, false
	})
	h := NewHandler(Config{
		Pool:     testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at1", ExpiresAt: 9999999999}),
		Upstream: up,
	})
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"glm-5.2","messages":[]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Errorf("code=%d body=%s", rec.Code, rec.Body)
	}
	var e map[string]any
	json.Unmarshal(rec.Body.Bytes(), &e)
	if e["error"] == nil {
		t.Errorf("want error envelope: %s", rec.Body)
	}
}

func TestChatSessionDeadDisables(t *testing.T) {
	up := newFakeUpstream(t, func(authz string) (int, string, bool) {
		return 401, `{"code":1001,"msg":"login required"}`, false
	})
	p := testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at1", ExpiresAt: 9999999999})
	h := NewHandler(Config{Pool: p, Upstream: up})
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"glm-5.2","messages":[]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Errorf("code=%d", rec.Code)
	}
	st, _ := p.Status("u1")
	if !st.Disabled {
		t.Errorf("account should be disabled: %+v", st)
	}
}

func TestChatUnknownModel400(t *testing.T) {
	h := NewHandler(Config{Pool: testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at", ExpiresAt: 9999999999}), Upstream: upstream.New()})
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"does-not-exist-xyz","messages":[]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Errorf("code=%d body=%s", rec.Code, rec.Body)
	}
	var e map[string]any
	json.Unmarshal(rec.Body.Bytes(), &e)
	if e["error"] == nil {
		t.Errorf("want error envelope: %s", rec.Body)
	}
}

func TestModelsEndpoint(t *testing.T) {
	h := NewHandler(Config{Pool: testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at", ExpiresAt: 9999999999}), Upstream: upstream.New()})
	req := httptest.NewRequest("GET", "/v1/models", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	var resp map[string]any
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["object"] != "list" {
		t.Errorf("object=%v", resp["object"])
	}
	data := resp["data"].([]any)
	if len(data) != 32 {
		t.Errorf("models count=%d want 32", len(data))
	}
	found := false
	for _, m := range data {
		if m.(map[string]any)["id"] == "glm-5.2" {
			found = true
		}
	}
	if !found {
		t.Error("glm-5.2 missing")
	}
}

func TestAPIKeyAuth(t *testing.T) {
	h := NewHandler(Config{
		Pool:     testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at", ExpiresAt: 9999999999}),
		Upstream: upstream.New(),
		APIKey:   "test-key",
	})
	// 无 key → 401
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("no key: code=%d", rec.Code)
	}
	// 错 key → 401
	req = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("wrong key: code=%d", rec.Code)
	}
	// 对 key → 200（models）
	req = httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("right key: code=%d", rec.Code)
	}
}

func TestRequestBodyTooLarge(t *testing.T) {
	h := NewHandler(Config{Pool: testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at", ExpiresAt: 9999999999}), Upstream: upstream.New()})
	big := strings.Repeat("a", maxBodyBytes+1)
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(big))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("code=%d body=%s", rec.Code, rec.Body)
	}
	var e map[string]any
	json.Unmarshal(rec.Body.Bytes(), &e)
	if e["error"] == nil {
		t.Errorf("want error envelope: %s", rec.Body)
	}
}

func TestAPIKeyAuthCaseInsensitivePrefix(t *testing.T) {
	h := NewHandler(Config{
		Pool:     testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at", ExpiresAt: 9999999999}),
		Upstream: upstream.New(),
		APIKey:   "test-key",
	})
	// 大小写不同的 Bearer 前缀也应接受（按规范，前缀大小写不敏感）。
	req := httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "bearer test-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("lowercase bearer: code=%d", rec.Code)
	}
	// 错误 key 仍拒绝。
	req = httptest.NewRequest("GET", "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("wrong key: code=%d", rec.Code)
	}
}

func TestStatusEndpoint(t *testing.T) {
	p := testPoolWith(&auth.Auth{UID: "u1", Nickname: "nick", AccessToken: "at", ExpiresAt: 9999999999})
	p.SetCredits("u1", 42)
	h := NewHandler(Config{Pool: p, Upstream: upstream.New()})
	req := httptest.NewRequest("GET", "/status", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code=%d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"uid":"u1"`) || !strings.Contains(body, `"credits":42`) {
		t.Errorf("body=%s", body)
	}
	if strings.Contains(body, "AccessToken") || strings.Contains(body, `"at"`) {
		t.Error("token leaked in status output")
	}
}

func TestHealthz(t *testing.T) {
	h := NewHandler(Config{Pool: pool.New(""), Upstream: upstream.New()})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/healthz", nil))
	if rec.Code != 200 {
		t.Errorf("code=%d", rec.Code)
	}
}

// TestImportEmptyDeviceAutoAssign — 新增账号(JSON 导入, 无 deviceId)时自动生成 16 位数字
// deviceId 并固化(pool + 落盘文件)。是本仓库「新增账号随机 deviceId 固化」的核心行为。
func TestImportEmptyDeviceAutoAssign(t *testing.T) {
	authDir := t.TempDir()
	up := newFakeUpstream(t, func(authz string) (int, string, bool) {
		return 200, `{}`, false
	})
	h := NewHandler(Config{
		Pool:         pool.New(""),
		Upstream:     up,
		AuthDir:      authDir,
		APIKey:       "k",
		PlanCooldown: 12 * time.Hour,
		SoftCooldown: time.Minute,
		ErrThreshold: 3,
		ErrCooldown:  10 * time.Minute,
		DefaultModel: upstream.DefaultConfigName,
	})

	// 扁平 JSON（无 deviceId）作为 {"json": "<string>"} 里的字符串，走 JSON 导入路径
	flat := `{"accessToken":"at","refreshToken":"rt","uid":"u-new","nickname":"NewUser"}`
	flatEsc, _ := json.Marshal(flat)
	payload := `{"json":` + string(flatEsc) + `}`
	req := httptest.NewRequest("POST", "/admin/api/accounts/import", strings.NewReader(payload))
	req.Header.Set("Authorization", "Bearer k")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("import code=%d body=%s", rec.Code, rec.Body.String())
	}

	// pool 中的 Auth 应有 16 位数字 deviceId
	a := h.cfg.Pool.AuthByUID("u-new")
	if a == nil {
		t.Fatal("account not in pool")
	}
	if !regexp.MustCompile(`^[1-9][0-9]{15}$`).MatchString(a.DeviceID) {
		t.Fatalf("auto-assigned deviceId invalid: %q", a.DeviceID)
	}
	// 落盘文件也固化
	raw, err := os.ReadFile(filepath.Join(authDir, "trae-u-new.json"))
	if err != nil {
		t.Fatalf("read persisted file: %v", err)
	}
	parsed, err := auth.Parse(raw)
	if err != nil {
		t.Fatalf("parse persisted: %v", err)
	}
	if parsed.DeviceID != a.DeviceID {
		t.Fatalf("persisted deviceId mismatch: pool=%q file=%q", a.DeviceID, parsed.DeviceID)
	}
}

// 模拟 Work 原生 SSE 响应
const mockWorkSSE = "event: plan_item\n" +
	"data: {\"event\":\"plan_item\",\"payload\":{\"thought\":\"work plan\",\"reasoning_content\":\"thinking\"}}\n\n" +
	"event: output\n" +
	"data: {\"event\":\"output\",\"payload\":{\"choices\":[{\"text\":\"Hello from Work!\"}]}}\n\n" +
	"event: token_usage\n" +
	"data: {\"event\":\"token_usage\",\"payload\":{\"total_tokens\":42}}\n\n" +
	"event: done\n" +
	"data: {\"event\":\"done\"}\n\n"

func TestWorkModelRoutingAndStreaming(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == upstream.EpCreateAgentTask {
			if r.Header.Get("Authorization") != "Cloud-IDE-JWT at-work" {
				t.Errorf("expected Cloud-IDE-JWT at-work, got %s", r.Header.Get("Authorization"))
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(mockWorkSSE))
			return
		}
		if r.URL.Path == upstream.EpChat {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"billing_mode\":\"credits\",\"cn_credits_remain_info\":{\"ide_credits\":0,\"work_credits\":888.0}}\n\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	workClient := upstream.NewWorkClient(upstream.WorkClientConfig{
		Host: ts.URL,
		Mode: upstream.WorkModeNative,
	})

	p := pool.New("")
	p.Add(&auth.Auth{UID: "u-work", AccessToken: "at-work", ExpiresAt: 9999999999})
	p.SetWorkCredits("u-work", 888.0)

	h := NewHandler(Config{
		Pool:       p,
		Upstream:   newFakeUpstream(t, func(auth string) (int, string, bool) { return 500, "solo should not be called", false }),
		WorkClient: workClient,
		WorkMode:   upstream.WorkModeNative,
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"DeepSeek-V4-Flash-Official","stream":true,"messages":[{"role":"user","content":"ping"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Hello from Work!") {
		t.Errorf("missing Work content in stream: %s", body)
	}
	if !strings.Contains(body, "[DONE]") {
		t.Errorf("missing [DONE] in stream: %s", body)
	}
}

func TestWorkModelMultiAccountRotationOnFailure(t *testing.T) {
	attempt := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == upstream.EpCreateAgentTask {
			attempt++
			authz := r.Header.Get("Authorization")
			if authz == "Cloud-IDE-JWT at-acct1" {
				// 第一个账号模拟 429 报错
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"code":1004,"message":"rate limit exceeded"}`))
				return
			}
			if authz == "Cloud-IDE-JWT at-acct2" {
				// 第二个账号成功
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte(mockWorkSSE))
				return
			}
		}
		if r.URL.Path == upstream.EpChat {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"billing_mode\":\"credits\",\"cn_credits_remain_info\":{\"ide_credits\":0,\"work_credits\":500.0}}\n\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	workClient := upstream.NewWorkClient(upstream.WorkClientConfig{
		Host: ts.URL,
		Mode: upstream.WorkModeNative,
	})

	p := pool.New("")
	// 账号 1 积分更高 (1000)，优先被挑选
	p.Add(&auth.Auth{UID: "acct1", AccessToken: "at-acct1", ExpiresAt: 9999999999})
	p.SetWorkCredits("acct1", 1000.0)

	// 账号 2 积分次之 (500)
	p.Add(&auth.Auth{UID: "acct2", AccessToken: "at-acct2", ExpiresAt: 9999999999})
	p.SetWorkCredits("acct2", 500.0)

	h := NewHandler(Config{
		Pool:         p,
		Upstream:     newFakeUpstream(t, func(auth string) (int, string, bool) { return 500, "solo should not be called", false }),
		WorkClient:   workClient,
		WorkMode:     upstream.WorkModeNative,
		SoftCooldown: 1 * time.Minute,
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"DeepSeek-V4-Flash-Official","stream":false,"messages":[{"role":"user","content":"test"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200 after rotation, got %d: %s", rec.Code, rec.Body)
	}

	if attempt != 2 {
		t.Errorf("expected 2 attempts across accounts, got %d", attempt)
	}

	// 验证 acct1 已被标记 WorkCooling
	st1, _ := p.Status("acct1")
	if !st1.WorkCooling {
		t.Errorf("acct1 should be cooling for work, got %+v", st1)
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	choices := resp["choices"].([]any)
	firstChoice := choices[0].(map[string]any)
	msg := firstChoice["message"].(map[string]any)
	if !strings.Contains(msg["content"].(string), "Hello from Work!") {
		t.Errorf("unexpected content: %v", msg["content"])
	}
}

func TestWorkModeDisabled(t *testing.T) {
	h := NewHandler(Config{
		Pool:     testPoolWith(&auth.Auth{UID: "u1", AccessToken: "at", ExpiresAt: 9999999999}),
		Upstream: newFakeUpstream(t, func(auth string) (int, string, bool) { return 200, soloSSE, true }),
		WorkMode: upstream.WorkModeDisabled,
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"DeepSeek-V4-Flash-Official","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when work mode is disabled, got %d: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "work_disabled") {
		t.Errorf("expected error code work_disabled, got %s", rec.Body)
	}
}

func TestSoloFallsBackToWorkOnPlanLimit(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == upstream.EpCreateAgentTask {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(mockWorkSSE))
			return
		}
		if r.URL.Path == upstream.EpChat {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"billing_mode\":\"credits\",\"cn_credits_remain_info\":{\"ide_credits\":0,\"work_credits\":50.0}}\n\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	workClient := upstream.NewWorkClient(upstream.WorkClientConfig{
		Host: ts.URL,
		Mode: upstream.WorkModeNative,
	})

	// SOLO 上游模拟 1005 (plan 权益不足)
	soloUp := newFakeUpstream(t, func(auth string) (int, string, bool) {
		return 400, `{"code":1005,"msg":"plan limit reached"}`, false
	})

	p := pool.New("")
	p.Add(&auth.Auth{UID: "u1", AccessToken: "at1", ExpiresAt: 9999999999})
	p.SetCredits("u1", 0)
	p.SetWorkCredits("u1", 50.0)

	h := NewHandler(Config{
		Pool:       p,
		Upstream:   soloUp,
		WorkClient: workClient,
		WorkMode:   upstream.WorkModeNative,
	})

	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"glm-5.2","messages":[{"role":"user","content":"hi"}]}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected fallback to 200, got %d: %s", rec.Code, rec.Body)
	}

	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["model"] != "glm-5.2" {
		t.Errorf("expected model=glm-5.2 in response, got %v", resp["model"])
	}
}

func TestAdminCreditsIncludesWorkCredits(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == upstream.EpChat {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: {\"billing_mode\":\"credits\",\"cn_credits_remain_info\":{\"ide_credits\":0,\"work_credits\":666.66}}\n\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer ts.Close()

	workClient := upstream.NewWorkClient(upstream.WorkClientConfig{
		Host: ts.URL,
		Mode: upstream.WorkModeNative,
	})

	p := pool.New("")
	p.Add(&auth.Auth{UID: "u1", Nickname: "TestUser", AccessToken: "at", ExpiresAt: 9999999999})

	h := NewHandler(Config{
		Pool:       p,
		Upstream:   upstream.New(),
		WorkClient: workClient,
		WorkMode:   upstream.WorkModeNative,
	})

	req := httptest.NewRequest("GET", "/admin/api/credits", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body)
	}

	var data map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	accts := data["accounts"].([]any)
	if len(accts) != 1 {
		t.Fatalf("expected 1 account, got %d", len(accts))
	}
	first := accts[0].(map[string]any)
	if first["work_credits"] != 666.66 {
		t.Errorf("expected work_credits 666.66, got %v", first["work_credits"])
	}
}

func TestSessionAffinityRouting(t *testing.T) {
	var chatAuths []string
	p := pool.New("")
	p.Add(&auth.Auth{UID: "u1", AccessToken: "at1", ExpiresAt: 9999999999})
	p.Add(&auth.Auth{UID: "u2", AccessToken: "at2", ExpiresAt: 9999999999})
	p.SetCredits("u1", 100)
	p.SetCredits("u2", 200)

	up := &upstream.Client{
		HTTP: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if strings.HasSuffix(r.URL.Path, upstream.EpChat) {
				chatAuths = append(chatAuths, r.Header.Get("Authorization"))
				return &http.Response{
					StatusCode: 200,
					Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
					Body:       io.NopCloser(strings.NewReader(soloSSE)),
				}, nil
			}
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{}`)),
			}, nil
		})},
	}

	h := NewHandler(Config{
		Pool:         p,
		Upstream:     up,
		DefaultModel: "glm-5.2",
	})

	// 轮次 1：包含首条消息
	req1 := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"glm-5.2","stream":true,"messages":[{"role":"user","content":"turn 1 hello"}]}`))
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	if rec1.Code != 200 {
		t.Fatalf("req1 failed: %d", rec1.Code)
	}

	// 此时动态提升 u1 的积分为 999（大于 u2 的 200）
	p.SetCredits("u1", 999)

	// 轮次 2：相同对话流后续提问（带相同的首轮消息特征）
	req2 := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"glm-5.2","stream":true,"messages":[{"role":"user","content":"turn 1 hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"turn 2"}]}`))
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("req2 failed: %d", rec2.Code)
	}

	if len(chatAuths) != 2 {
		t.Fatalf("expected 2 chat requests, got %d", len(chatAuths))
	}
	// 验证第二轮由于会话特征指纹粘性，仍然锁定在初始选择的 at2 上，未跳跃至积分更高的 at1
	if chatAuths[0] != "Cloud-IDE-JWT at2" || chatAuths[1] != "Cloud-IDE-JWT at2" {
		t.Fatalf("affinity failed: got %v, expected both to be at2", chatAuths)
	}
}
