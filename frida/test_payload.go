//go:build ignore

package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type AccountAuthFile struct {
	Account struct {
		UID string `json:"uid"`
	} `json:"account"`
	Auth struct {
		AccessToken string `json:"accessToken"`
		DeviceID    string `json:"deviceId"`
		MachineID   string `json:"machineId"`
	} `json:"auth"`
}

func main() {
	data, _ := os.ReadFile("/Users/jeff/project/traework/auths/trae-4122512616609817.json")
	var auth AccountAuthFile
	_ = json.Unmarshal(data, &auth)

	tr := &http.Transport{
		TLSClientConfig:   &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"h2"}},
		ForceAttemptHTTP2: true,
	}
	client := &http.Client{Transport: tr, Timeout: 30 * time.Second}

	payload := map[string]any{
		"conversation_id": fmt.Sprintf("conv-%d", time.Now().UnixNano()),
		"session_id":      fmt.Sprintf("sess-%d", time.Now().UnixNano()),
		"user_id":         auth.Account.UID,
		"device_id":       auth.Auth.DeviceID,
		"agent_type":      "solo_work_lite",
		"model_name":      "DeepSeek-V4-Flash-Official__dev",
		"config_name":     "DeepSeek-V4-Flash-Official",
		"ide_version":     "0.1.63",
		"version_code":    20260901,
		"mode_type":       1,
		"plugin_channel":  "stable",
		"history_id_list": []string{},
		"user_input": map[string]any{
			"id":    fmt.Sprintf("msg-%d", time.Now().UnixNano()),
			"query": "[{\"type\":\"text\",\"data\":{\"content\":\"hello\"}}]",
		},
	}

	raw, _ := json.Marshal(payload)
	req, _ := http.NewRequest("POST", "https://api5-normal.mchost.guru/api/agent/v3/create_agent_task", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream, application/json")
	req.Header.Set("User-Agent", "TraeClient/TTNet")
	req.Header.Set("Authorization", "Cloud-IDE-JWT "+auth.Auth.AccessToken)
	req.Header.Set("X-Cloudide-Token", auth.Auth.AccessToken)
	req.Header.Set("X-Ide-Token", auth.Auth.AccessToken)
	req.Header.Set("X-Uid", auth.Account.UID)
	req.Header.Set("X-Device-Id", auth.Auth.DeviceID)
	req.Header.Set("X-Machine-Id", auth.Auth.MachineID)
	req.Header.Set("X-App-Id", "6eefa01c-1036-4c7e-9ca5-d891f63bfcd8")
	req.Header.Set("X-App-Version", "default")
	req.Header.Set("X-App-Version-Code", "20260901")
	req.Header.Set("X-Ide-Version", "0.1.63")
	req.Header.Set("X-Ide-Version-Code", "20260901")
	req.Header.Set("X-Version-Code", "20260901")
	req.Header.Set("Request-Traffic-Type", "prod")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()
	fmt.Println("Status:", resp.StatusCode, resp.Header.Get("Content-Type"))
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) != "" {
			fmt.Println(" ", line)
		}
	}
}
