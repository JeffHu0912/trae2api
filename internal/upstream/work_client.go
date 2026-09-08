// work_client.go Trae Work 专有通道客户端（方案二：Zero-Electron 纯 Go 客户端）。
//
// 对应规范：
//   - docs/PLAN-SCHEME-2-PURE-PROTOCOL.md (阶段三交付物)
//   - docs/work-wire-protocol-spec.md (协议规范)
//   - docs/work-crypto-algorithm.md (签名与协议降级规范)
//
// 核心能力：
//   1. HTTP/2 原生连接池与 ALPN h2 平滑降级通信 (api5-normal.mchost.guru)
//   2. 基于 Cloud-IDE-JWT 与设备指纹矩阵的全要素 Header 组装器
//   3. Work 原生下行事件流 (plan_item, output, token_usage, done) 到标准 OpenAI SSE 规范流式转换
//   4. 实时额度探针：双通道积分余额探测 (ProbeCredits)
//   5. 支持 Native (直接协议) 与 Bridge (本地桥接回退) 双模式无缝切换
package upstream

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"trae2api/internal/auth"
)

const (
	// WorkTargetHost 官方 Work 专属端点 (volc-dcdn)
	WorkTargetHost = "https://api5-normal.mchost.guru"
	// WorkSoloHost SOLO 备选接入点
	WorkSoloHost = "https://trae-api-cn.mchost.guru"

	// WorkAppID 官方 Work 专属 AppID
	WorkAppID = "931506"
	// WorkAppIDChat 兼容部分内部路由及 llm_utils_chat 的 AppID
	WorkAppIDChat = "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8"

	WorkIdeVersion     = "0.1.63"
	WorkIdeVersionCode = "20260901"
	WorkAgentType      = "solo_work_lite"
	DefaultWorkModel   = "DeepSeek-V4-Flash-Official"

	// 核心接口端点
	EpCreateAgentTask = "/api/agent/v3/create_agent_task"
	EpWorkflowStart   = "/api/agent/v3/workflow/start"
	EpQueryHistory    = "/api/agent/v3/query_history_state"
	EpSyncHistory     = "/api/agent/v3/sync_history_state"

	// 本地桥接缺省配置 (用于平滑回退)
	DefaultWorkBridgeURL   = "http://127.0.0.1:7865"
	DefaultWorkBridgeToken = "twbridge-local-7f3a"
)

// WorkMode 客户端运行模式
type WorkMode string

const (
	WorkModeAuto     WorkMode = "auto"     // 智能自动模式：优先 Native，失败平滑降级 Bridge
	WorkModeNative   WorkMode = "native"   // 纯协议原生直连模式
	WorkModeBridge   WorkMode = "bridge"   // 外部 Bridge 桥接兼容模式
	WorkModeDisabled WorkMode = "disabled" // 禁用 Work 通道
)

// WorkClientConfig 客户端配置
type WorkClientConfig struct {
	Host        string
	BridgeURL   string
	BridgeToken string
	Mode        WorkMode
	Timeout     time.Duration
}

// DefaultWorkClientConfig 默认配置，支持环境变量覆盖
func DefaultWorkClientConfig() WorkClientConfig {
	host := strings.TrimRight(os.Getenv("TW2A_WORK_HOST"), "/")
	if host == "" {
		host = WorkTargetHost
	}

	bridgeURL := strings.TrimRight(os.Getenv("TW2A_WORK_BRIDGE_URL"), "/")
	if bridgeURL == "" {
		bridgeURL = DefaultWorkBridgeURL
	}

	bridgeToken := os.Getenv("TW2A_WORK_BRIDGE_TOKEN")
	if bridgeToken == "" {
		bridgeToken = DefaultWorkBridgeToken
	}

	modeStr := strings.ToLower(strings.TrimSpace(os.Getenv("TW2A_WORK_MODE")))
	var mode WorkMode
	switch modeStr {
	case "native":
		mode = WorkModeNative
	case "bridge":
		mode = WorkModeBridge
	case "disabled":
		mode = WorkModeDisabled
	default:
		mode = WorkModeAuto
	}

	return WorkClientConfig{
		Host:        host,
		BridgeURL:   bridgeURL,
		BridgeToken: bridgeToken,
		Mode:        mode,
		Timeout:     30 * time.Second,
	}
}

// WorkClient 纯 Go Work 通道客户端
type WorkClient struct {
	cfg        WorkClientConfig
	http       *http.Client // 带超时短请求客户端（探针/心跳）
	streamHTTP *http.Client // 无超时长流客户端（SSE 持续流式）
}

// NewWorkClient 构建 WorkClient，初始化 HTTP/2 连接池
func NewWorkClient(configs ...WorkClientConfig) *WorkClient {
	cfg := DefaultWorkClientConfig()
	if len(configs) > 0 {
		c := configs[0]
		if c.Host != "" {
			cfg.Host = strings.TrimRight(c.Host, "/")
		}
		if c.BridgeURL != "" {
			cfg.BridgeURL = strings.TrimRight(c.BridgeURL, "/")
		}
		if c.BridgeToken != "" {
			cfg.BridgeToken = c.BridgeToken
		}
		if c.Mode != "" {
			cfg.Mode = c.Mode
		}
		if c.Timeout > 0 {
			cfg.Timeout = c.Timeout
		}
	}

	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			NextProtos:         []string{"h2"},
			InsecureSkipVerify: false,
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
	}

	return &WorkClient{
		cfg:        cfg,
		http:       &http.Client{Transport: tr, Timeout: cfg.Timeout},
		streamHTTP: &http.Client{Transport: tr},
	}
}

// Config 返回当前配置副本
func (c *WorkClient) Config() WorkClientConfig {
	return c.cfg
}

// Mode 返回当前工作模式
func (c *WorkClient) Mode() WorkMode {
	return c.cfg.Mode
}

// ApplyWorkHeaders 根据规范注入全套鉴权头与设备指纹 Header 矩阵
func ApplyWorkHeaders(req *http.Request, a *auth.Auth, stream bool, useChatAppID bool) {
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream, application/json")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("User-Agent", "Trae/"+WorkIdeVersion)
	if useChatAppID || req.URL != nil && (req.URL.Path == EpCreateAgentTask || req.URL.Path == EpChat) {
		req.Header.Set("X-App-Id", WorkAppIDChat)
	} else {
		req.Header.Set("X-App-Id", WorkAppID)
	}
	req.Header.Set("X-Ide-Version", WorkIdeVersion)
	req.Header.Set("X-Ide-Version-Code", WorkIdeVersionCode)
	req.Header.Set("X-App-Version-Code", WorkIdeVersionCode)
	req.Header.Set("X-Version-Code", WorkIdeVersionCode)
	req.Header.Set("X-Device-Type", "macos")
	req.Header.Set("X-Device-Platform", "darwin")
	req.Header.Set("X-Platform", "darwin")
	req.Header.Set("X-OS", "darwin")
	req.Header.Set("X-OSType", "darwin")
	req.Header.Set("X-System", "darwin")
	req.Header.Set("Request-Traffic-Type", "prod")

	if a != nil {
		jwt := a.JWT()
		req.Header.Set("Authorization", "Cloud-IDE-JWT "+jwt)
		req.Header.Set("X-Cloudide-Token", jwt)
		req.Header.Set("X-Ide-Token", jwt)
		if a.UID != "" {
			req.Header.Set("X-Uid", a.UID)
		}
		if a.DeviceID != "" {
			req.Header.Set("X-Device-Id", a.DeviceID)
		}
		if a.MachineID != "" {
			req.Header.Set("X-Machine-Id", a.MachineID)
		}
	}
}

// RandomHex 生成指定字节数的随机 Hex 字符串（用于 UUID / Request ID）
func RandomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// RandomUUID 生成形如 8-4-4-4-12 的标准 UUID
func RandomUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// ExtractLastUserPrompt 从 OpenAI messages 列表中提取最后一条 user 输入
func ExtractLastUserPrompt(messages []any) string {
	for i := len(messages) - 1; i >= 0; i-- {
		m, ok := messages[i].(map[string]any)
		if !ok {
			continue
		}
		if role, _ := m["role"].(string); role == "user" {
			switch c := m["content"].(type) {
			case string:
				if strings.TrimSpace(c) != "" {
					return c
				}
			case []any:
				var sb strings.Builder
				for _, part := range c {
					if pMap, ok := part.(map[string]any); ok {
						if t, ok := pMap["text"].(string); ok {
							sb.WriteString(t)
						}
					}
				}
				if sb.Len() > 0 {
					return sb.String()
				}
			}
		}
	}
	return "你好"
}

// BuildNativeTaskPayload 构造官方 create_agent_task 的原生上行 JSON 请求体
func BuildNativeTaskPayload(a *auth.Auth, model, prompt, convID, sessID string) []byte {
	if convID == "" {
		convID = RandomUUID()
	}
	if sessID == "" {
		sessID = RandomUUID()
	}
	msgID := RandomUUID()

	// 格式化 query 为 JSON 文本切片
	queryJSON, _ := json.Marshal([]map[string]any{
		{
			"type": "text",
			"data": map[string]string{
				"content": prompt,
			},
		},
	})

	internalModelName := model
	if !strings.HasSuffix(internalModelName, "__dev") {
		internalModelName = model + "__dev"
	}

	payload := map[string]any{
		"conversation_id": convID,
		"session_id":      sessID,
		"user_id":         a.UID,
		"device_id":       a.DeviceID,
		"agent_type":      WorkAgentType,
		"model_name":      internalModelName,
		"config_name":     model,
		"ide_version":     WorkIdeVersion,
		"version_code":    20260901,
		"mode_type":       1,
		"plugin_channel":  "stable",
		"history_id_list": []string{},
		"user_input": map[string]any{
			"id":    msgID,
			"query": string(queryJSON),
			"messages": []map[string]any{
				{
					"role":    "user",
					"content": prompt,
				},
			},
		},
	}

	raw, _ := json.Marshal(payload)
	return raw
}

// WorkCreditsSnapshot 积分快照
type WorkCreditsSnapshot struct {
	IdeCredits  float64 `json:"ide_credits"`
	WorkCredits float64 `json:"work_credits"`
}

// ProbeCredits 探测当前账号的双通道积分实时余额 (ide_credits vs work_credits)
func (c *WorkClient) ProbeCredits(ctx context.Context, a *auth.Auth) (*WorkCreditsSnapshot, error) {
	pingPayload := map[string]any{
		"function":    WorkAgentType,
		"config_name": DefaultWorkModel,
		"model":       DefaultWorkModel,
		"stream":      true,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": "ping"},
				},
			},
		},
	}
	raw, _ := json.Marshal(pingPayload)

	reqURL := c.cfg.Host + EpChat
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	ApplyWorkHeaders(req, a, true, true)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("probe credits transport error: %w", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			rawJSON := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			var data map[string]any
			if err := json.Unmarshal([]byte(rawJSON), &data); err == nil {
				if remain, ok := data["cn_credits_remain_info"].(map[string]any); ok {
					var snap WorkCreditsSnapshot
					if v, ok := remain["ide_credits"].(float64); ok {
						snap.IdeCredits = v
					}
					if v, ok := remain["work_credits"].(float64); ok {
						snap.WorkCredits = v
					}
					return &snap, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("no cn_credits_remain_info in probe response")
}

// ChatStream 执行 Work 通道请求并返回原始流式 Reader。
// 在 Auto 模式下优先尝试原生调用，必要时回退至本地 Bridge。
func (c *WorkClient) ChatStream(ctx context.Context, a *auth.Auth, body []byte) (rc io.ReadCloser, status int, respBody []byte, err error) {
	// 0. 禁用模式检查
	if c.cfg.Mode == WorkModeDisabled {
		return nil, http.StatusForbidden, []byte(`{"error":{"message":"work channel is disabled by configuration (TW2A_WORK_MODE=disabled)","type":"work_disabled"}}`), fmt.Errorf("work channel is disabled")
	}

	var peek struct {
		Model    string `json:"model"`
		Stream   bool   `json:"stream"`
		Messages []any  `json:"messages"`
	}
	_ = json.Unmarshal(body, &peek)

	model := peek.Model
	if model == "" || model == "auto" || model == "default" {
		model = DefaultWorkModel
	}

	// 1. 如果显式配置走 Bridge 模式，直接代理至本地 WorkBridge
	if c.cfg.Mode == WorkModeBridge {
		return c.doBridgeStream(ctx, body)
	}

	// 2. Native 模式：尝试原生发送至 api5-normal 的 create_agent_task
	prompt := ExtractLastUserPrompt(peek.Messages)
	nativeBody := BuildNativeTaskPayload(a, model, prompt, "", "")
	reqURL := c.cfg.Host + EpCreateAgentTask

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(nativeBody))
	if err != nil {
		return nil, 0, nil, err
	}
	ApplyWorkHeaders(req, a, true, false)

	resp, err := c.streamHTTP.Do(req)
	if err != nil {
		if c.cfg.Mode == WorkModeAuto {
			log.Printf("[WorkClient] native request failed (%v), falling back to bridge", err)
			return c.doBridgeStream(ctx, body)
		}
		return nil, 0, nil, err
	}

	// 读取前若干字节排查是否包含业务级错误
	peekBuf := make([]byte, 1024)
	n, readErr := resp.Body.Read(peekBuf)
	combinedReader := io.MultiReader(bytes.NewReader(peekBuf[:n]), resp.Body)

	// 如果首帧返回包含业务错误 (例如 code: 4001 / failed to get summary config)
	firstBytes := string(peekBuf[:n])
	if strings.Contains(firstBytes, `"code":4001`) || strings.Contains(firstBytes, `"code":4000105`) {
		_ = resp.Body.Close()
		if c.cfg.Mode == WorkModeAuto {
			log.Printf("[WorkClient] native server returned error (%s), fallback to bridge", strings.TrimSpace(firstBytes))
			return c.doBridgeStream(ctx, body)
		}
		return nil, http.StatusBadRequest, []byte(firstBytes), nil
	}

	if readErr == io.EOF {
		readErr = nil
	}

	// 返回组合 Reader 保证首帧不丢失
	return &multiReadCloser{Reader: combinedReader, Closer: resp.Body}, resp.StatusCode, nil, readErr
}

func (c *WorkClient) doBridgeStream(ctx context.Context, body []byte) (io.ReadCloser, int, []byte, error) {
	targetURL := c.cfg.BridgeURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")
	if c.cfg.BridgeToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.BridgeToken)
	}

	resp, err := c.streamHTTP.Do(req)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("bridge stream error: %w", err)
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		return nil, resp.StatusCode, raw, nil
	}
	return resp.Body, resp.StatusCode, nil, nil
}

// StreamWorkToOpenAI 将 Work 下行 SSE 流式数据转换并推送为标准 OpenAI chat.completion.chunk
func StreamWorkToOpenAI(w http.ResponseWriter, r io.Reader, model, cmplID string) error {
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")

	fl, _ := w.(http.Flusher)
	if cmplID == "" {
		cmplID = "chatcmpl-" + RandomHex(12)
	}

	br := bufio.NewReaderSize(r, 64*1024)
	var fullText string
	var pendingUsage map[string]any
	sawDone := false

	handleDelta := func(text string) string {
		if text == "" {
			return ""
		}
		if strings.HasPrefix(text, fullText) && len(text) > len(fullText) {
			delta := text[len(fullText):]
			fullText = text
			return delta
		} else if !strings.Contains(fullText, text) {
			fullText += text
			return text
		}
		return ""
	}

	writeChunk := func(deltaText string, reasoningText string, finish string) error {
		delta := map[string]any{}
		if deltaText != "" {
			delta["content"] = deltaText
		}
		if reasoningText != "" {
			delta["reasoning_content"] = reasoningText
		}

		choice := map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": nil,
		}
		if finish != "" {
			choice["finish_reason"] = finish
		}

		chunk := map[string]any{
			"id":      cmplID,
			"object":  "chat.completion.chunk",
			"created": time.Now().Unix(),
			"model":   model,
			"choices": []any{choice},
		}
		if pendingUsage != nil {
			chunk["usage"] = pendingUsage
			pendingUsage = nil
		}

		raw, _ := json.Marshal(chunk)
		if _, err := io.WriteString(w, "data: "+string(raw)+"\n\n"); err != nil {
			return err
		}
		if fl != nil {
			fl.Flush()
		}
		return nil
	}

	writeDONE := func() error {
		if _, err := io.WriteString(w, "data: [DONE]\n\n"); err != nil {
			return err
		}
		if fl != nil {
			fl.Flush()
		}
		return nil
	}

	var currentEvent string
	for {
		line, err := br.ReadString('\n')
		if err != nil && err != io.EOF {
			return err
		}
		trimmed := strings.TrimRight(line, "\r\n")

		if strings.HasPrefix(trimmed, "event:") {
			currentEvent = strings.TrimSpace(strings.TrimPrefix(trimmed, "event:"))
		} else if strings.HasPrefix(trimmed, "data:") {
			dataContent := strings.TrimPrefix(trimmed, "data:")
			dataContentTrimmed := strings.TrimSpace(dataContent)

			// 如果已经是标准 OpenAI [DONE]，直接透传
			if dataContentTrimmed == "[DONE]" {
				sawDone = true
				_ = writeDONE()
				break
			}

			// 如果已经是标准 OpenAI chunk，直接透传回客户端
			if strings.Contains(dataContentTrimmed, `"chat.completion.chunk"`) {
				if _, werr := io.WriteString(w, trimmed+"\n\n"); werr != nil {
					return werr
				}
				if fl != nil {
					fl.Flush()
				}
				if err == io.EOF {
					break
				}
				continue
			}

			// 否则按 Work 原生事件流解析
			var evObj struct {
				Event   string `json:"event"`
				Code    int    `json:"code"`
				Message string `json:"message"`
				Data    struct {
					Event   string          `json:"event"`
					Payload json.RawMessage `json:"payload"`
				} `json:"data"`
				Payload json.RawMessage `json:"payload"`
			}
			if jerr := json.Unmarshal([]byte(dataContentTrimmed), &evObj); jerr == nil {
				ev := evObj.Event
				if ev == "" {
					ev = evObj.Data.Event
				}
				if ev == "" {
					ev = currentEvent
				}

				payloadRaw := evObj.Payload
				if len(payloadRaw) == 0 {
					payloadRaw = evObj.Data.Payload
				}

				switch ev {
				case "plan_item":
					var plan struct {
						Thought          string `json:"thought"`
						ReasoningContent string `json:"reasoning_content"`
						ToolCallInfo     struct {
							Params struct {
								Summary string `json:"summary"`
							} `json:"params"`
						} `json:"tool_call_info"`
					}
					if len(payloadRaw) > 0 && json.Unmarshal(payloadRaw, &plan) == nil {
						if plan.ReasoningContent != "" {
							if d := handleDelta(plan.ReasoningContent); d != "" {
								_ = writeChunk("", d, "")
							}
						}
						if plan.Thought != "" {
							if d := handleDelta(plan.Thought); d != "" {
								_ = writeChunk(d, "", "")
							}
						}
						if plan.ToolCallInfo.Params.Summary != "" {
							if d := handleDelta(plan.ToolCallInfo.Params.Summary); d != "" {
								_ = writeChunk(d, "", "")
							}
						}
					}

				case "output":
					var out struct {
						Choices []struct {
							Text string `json:"text"`
						} `json:"choices"`
					}
					if len(payloadRaw) > 0 && json.Unmarshal(payloadRaw, &out) == nil {
						for _, choice := range out.Choices {
							if choice.Text != "" && choice.Text != "[]" {
								if d := handleDelta(choice.Text); d != "" {
									_ = writeChunk(d, "", "")
								}
							}
						}
					}

				case "token_usage":
					var usage map[string]any
					if len(payloadRaw) > 0 && json.Unmarshal(payloadRaw, &usage) == nil {
						pendingUsage = usage
					}

				case "done":
					var donePayload struct {
						LastAssistantResponse string `json:"last_assistant_response"`
					}
					if len(payloadRaw) > 0 && json.Unmarshal(payloadRaw, &donePayload) == nil {
						if donePayload.LastAssistantResponse != "" {
							var arr []string
							if json.Unmarshal([]byte(donePayload.LastAssistantResponse), &arr) == nil && len(arr) > 0 {
								if d := handleDelta(arr[0]); d != "" {
									_ = writeChunk(d, "", "")
								}
							}
						}
					}
					_ = writeChunk("", "", "stop")
					_ = writeDONE()
					sawDone = true

				case "error":
					errMsg := evObj.Message
					if errMsg == "" {
						errMsg = "upstream work stream error"
					}
					_, _ = io.WriteString(w, fmt.Sprintf("event: error\ndata: %q\n\n", errMsg))
					_ = writeDONE()
					sawDone = true
				}
			}
		}

		if err == io.EOF {
			break
		}
	}

	if !sawDone {
		return writeDONE()
	}
	return nil
}

// AggregateWork 将完整 Work SSE 流聚合成非流式单个 OpenAI 响应格式
func AggregateWork(r io.Reader, model, cmplID string) (map[string]any, error) {
	if cmplID == "" {
		cmplID = "chatcmpl-" + RandomHex(12)
	}

	rawBytes, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(rawBytes)
	// 兼容已有完整 OpenAI JSON 响应（如某些桥接下发的原始 JSON）
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var directResp map[string]any
		if jerr := json.Unmarshal(trimmed, &directResp); jerr == nil {
			if _, hasChoices := directResp["choices"]; hasChoices {
				return directResp, nil
			}
		}
	}

	var (
		fullContent strings.Builder
		usage       map[string]any
		fullText    string
	)

	handleDelta := func(text string) {
		if text == "" {
			return
		}
		if strings.HasPrefix(text, fullText) && len(text) > len(fullText) {
			delta := text[len(fullText):]
			fullText = text
			fullContent.WriteString(delta)
		} else if !strings.Contains(fullText, text) {
			fullText += text
			fullContent.WriteString(text)
		}
	}

	scanner := bufio.NewScanner(bytes.NewReader(trimmed))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data:") {
			dataTrimmed := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if dataTrimmed == "[DONE]" {
				break
			}
			var evObj struct {
				Event   string          `json:"event"`
				Payload json.RawMessage `json:"payload"`
				Data    struct {
					Event   string          `json:"event"`
					Payload json.RawMessage `json:"payload"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(dataTrimmed), &evObj); err == nil {
				ev := evObj.Event
				if ev == "" {
					ev = evObj.Data.Event
				}
				p := evObj.Payload
				if len(p) == 0 {
					p = evObj.Data.Payload
				}

				switch ev {
				case "plan_item":
					var plan struct {
						Thought      string `json:"thought"`
						ToolCallInfo struct {
							Params struct {
								Summary string `json:"summary"`
							} `json:"params"`
						} `json:"tool_call_info"`
					}
					if len(p) > 0 && json.Unmarshal(p, &plan) == nil {
						if plan.Thought != "" {
							handleDelta(plan.Thought)
						}
						if plan.ToolCallInfo.Params.Summary != "" {
							handleDelta(plan.ToolCallInfo.Params.Summary)
						}
					}
				case "output":
					var out struct {
						Choices []struct {
							Text string `json:"text"`
						} `json:"choices"`
					}
					if len(p) > 0 && json.Unmarshal(p, &out) == nil {
						for _, c := range out.Choices {
							if c.Text != "" && c.Text != "[]" {
								handleDelta(c.Text)
							}
						}
					}
				case "token_usage":
					var u map[string]any
					if len(p) > 0 && json.Unmarshal(p, &u) == nil {
						usage = u
					}
				}
			}
		}
	}

	resp := map[string]any{
		"id":      cmplID,
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": fullContent.String(),
				},
				"finish_reason": "stop",
			},
		},
	}
	if usage != nil {
		resp["usage"] = usage
	}
	return resp, nil
}

type multiReadCloser struct {
	io.Reader
	io.Closer
}
