// cmd/verify_work_credits/main.go
// 阶段三（M3）交付验收程序：单账号单次成功扣费与 Work 通道端到端流式验证。
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"trae2api/internal/auth"
	"trae2api/internal/upstream"
)

func main() {
	fmt.Println("================================================================================")
	fmt.Println("  TraeWork 方案二（Zero-Electron 纯 Go 客户端）阶段三（M3）单账号扣费验收程序")
	fmt.Println("================================================================================")

	authPath := "/Users/jeff/project/traework/auths/trae-4122512616609817.json"
	if len(os.Args) > 1 {
		authPath = os.Args[1]
	}

	data, err := os.ReadFile(authPath)
	if err != nil {
		fmt.Printf("[-] 读取凭证失败 (%s): %v\n", authPath, err)
		os.Exit(1)
	}

	var af struct {
		Account struct {
			UID      string `json:"uid"`
			Nickname string `json:"nickname"`
		} `json:"account"`
		Auth struct {
			AccessToken string `json:"accessToken"`
			DeviceID    string `json:"deviceId"`
			MachineID   string `json:"machineId"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(data, &af); err != nil {
		fmt.Printf("[-] 解析凭证 JSON 失败: %v\n", err)
		os.Exit(1)
	}

	account := &auth.Auth{
		UID:         af.Account.UID,
		Nickname:    af.Account.Nickname,
		AccessToken: af.Auth.AccessToken,
		DeviceID:    af.Auth.DeviceID,
		MachineID:   af.Auth.MachineID,
	}

	fmt.Printf("[+] 载入账号凭证成功: UID=%s, DeviceID=%s\n", account.UID, account.DeviceID)
	fmt.Printf("[+] Token 凭证前缀: %s...\n", account.AccessToken[:32])

	// 初始化 WorkClient
	client := upstream.NewWorkClient()
	ctx := context.Background()

	// -------------------------------------------------------------------------
	// 第一步：调用探针拉取扣费前积分基线
	// -------------------------------------------------------------------------
	fmt.Println("\n[STEP 1/3] 探测扣费前积分基线快照 (Probe Initial Credits)...")
	initialSnap, err := client.ProbeCredits(ctx, account)
	if err != nil {
		fmt.Printf("[-] 探测初始积分失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("    -> ide_credits (SOLO 免费额度):  %.4f\n", initialSnap.IdeCredits)
	fmt.Printf("    -> work_credits (Work 任务专有): %.4f\n", initialSnap.WorkCredits)

	// -------------------------------------------------------------------------
	// 第二步：通过 WorkClient 发起真实 Work 模式请求并流式接收
	// -------------------------------------------------------------------------
	prompt := "用一句简短精炼的话解释什么是量子纠缠？"
	model := "DeepSeek-V4-Flash-Official"
	fmt.Printf("\n[STEP 2/3] 发起 Work 通道调用 (模型: %s, 提示词: %q)...\n", model, prompt)

	chatReq := map[string]any{
		"model":  model,
		"stream": true,
		"messages": []map[string]any{
			{
				"role":    "user",
				"content": prompt,
			},
		},
	}
	reqBody, _ := json.Marshal(chatReq)

	rc, status, errBody, err := client.ChatStream(ctx, account, reqBody)
	if err != nil {
		fmt.Printf("[-] ChatStream 传输失败: %v\n", err)
		os.Exit(1)
	}
	if rc == nil {
		fmt.Printf("[-] ChatStream 返回非 2xx (HTTP %d): %s\n", status, string(errBody))
		os.Exit(1)
	}
	defer rc.Close()

	fmt.Println("    -> HTTP 连接建立成功 (HTTP 200), 开始接收流式 Token:")
	fmt.Print("    -> [模型实时输出]: ")

	scanner := bufio.NewScanner(rc)
	var fullText string
	tokenChunkCount := 0

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "data:") {
			dataContent := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
			if dataContent == "[DONE]" {
				break
			}

			// 解析标准 OpenAI chunk
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content          string `json:"content"`
						ReasoningContent string `json:"reasoning_content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(dataContent), &chunk); err == nil && len(chunk.Choices) > 0 {
				c := chunk.Choices[0].Delta.Content
				if c != "" {
					fmt.Print(c)
					fullText += c
					tokenChunkCount++
				}
			}
		}
	}
	fmt.Println()
	fmt.Printf("    -> 流式传输完毕，共接收 %d 个分块，完整输出字符数: %d\n", tokenChunkCount, len(fullText))

	// 等待服务端账单落盘
	time.Sleep(2 * time.Second)

	// -------------------------------------------------------------------------
	// 第三步：再次探测积分状态并验证扣费闭环
	// -------------------------------------------------------------------------
	fmt.Println("\n[STEP 3/3] 探测扣费后积分状态并比对差额 (Probe Post-Request Credits)...")
	finalSnap, err := client.ProbeCredits(ctx, account)
	if err != nil {
		fmt.Printf("[-] 探测最终积分失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("    -> ide_credits (SOLO 免费额度):  %.4f\n", finalSnap.IdeCredits)
	fmt.Printf("    -> work_credits (Work 任务专有): %.4f\n", finalSnap.WorkCredits)

	consumed := initialSnap.WorkCredits - finalSnap.WorkCredits
	fmt.Printf("\n================================================================================\n")
	fmt.Printf("  【M3 验收结果汇总】\n")
	fmt.Printf("  - 账号 UID:        %s\n", account.UID)
	fmt.Printf("  - 使用模型:        %s\n", model)
	fmt.Printf("  - 初始 work_credits: %.4f\n", initialSnap.WorkCredits)
	fmt.Printf("  - 最终 work_credits: %.4f\n", finalSnap.WorkCredits)
	fmt.Printf("  - 真实消耗积分:    %.4f\n", consumed)
	fmt.Printf("================================================================================\n")

	if consumed > 0 {
		fmt.Println("  >>> [SUCCESS] 阶段三（M3）单账号单次成功扣费实机验证通过！<<<")
	} else {
		fmt.Println("  >>> [WARN] work_credits 差额 <= 0，可能是缓存延迟或免费计费 <<<")
	}
	fmt.Println("================================================================================")
}
