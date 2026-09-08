//go:build ignore

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"trae2api/internal/auth"
	"trae2api/internal/upstream"
)

func main() {
	data, err := os.ReadFile("/Users/jeff/project/traework/auths/trae-4122512616609817.json")
	if err != nil {
		fmt.Println("ReadFile error:", err)
		return
	}
	var af struct {
		Account struct {
			UID string `json:"uid"`
		} `json:"account"`
		Auth struct {
			AccessToken string `json:"accessToken"`
			DeviceID    string `json:"deviceId"`
			MachineID   string `json:"machineId"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(data, &af); err != nil {
		fmt.Println("Unmarshal error:", err)
		return
	}
	a := &auth.Auth{
		UID:         af.Account.UID,
		AccessToken: af.Auth.AccessToken,
		DeviceID:    af.Auth.DeviceID,
		MachineID:   af.Auth.MachineID,
	}
	c := upstream.New()
	models, err := c.FetchModels(a)
	if err != nil {
		fmt.Println("FetchModels error:", err)
		return
	}
	fmt.Printf("Fetched %d models:\n", len(models))
	for _, m := range models {
		fmt.Printf("ID: %s | Name: %s\n", m.ID, m.Name)
	}
}
