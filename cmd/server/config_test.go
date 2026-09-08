package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefault(t *testing.T) {
	c := Default()
	if c.Listen != ":7864" {
		t.Errorf("listen=%s", c.Listen)
	}
	if c.DefaultModel != "glm-5.2" {
		t.Errorf("default_model=%s", c.DefaultModel)
	}
	if err := c.normalize(); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if c.PlanCreditDur.Hours() != 12 {
		t.Errorf("plan_credit=%v", c.PlanCreditDur)
	}
	if c.SoftRateDur.Seconds() != 60 {
		t.Errorf("soft_rate=%v", c.SoftRateDur)
	}
}

func TestLoadFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "c.json")
	os.WriteFile(fp, []byte(`{"listen":":9999","default_model":"kimi-k2.7-code","schedule":{"checkin_hour":7}}`), 0o600)
	c, err := Load(fp)
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":9999" || c.DefaultModel != "kimi-k2.7-code" || c.Schedule.CheckinHour != 7 {
		t.Errorf("c=%+v", c)
	}
	// APIKey 不读 json
	if c.APIKey != "" {
		t.Errorf("api_key should not come from json, got %q", c.APIKey)
	}
}

func TestLoadMissingFileFallsBackToDefaults(t *testing.T) {
	c, err := Load("/nonexistent/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":7864" {
		t.Errorf("listen=%s", c.Listen)
	}
}

func TestAPIKeyOnlyFromEnv(t *testing.T) {
	t.Setenv("TW2A_API_KEY", "test-key")
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.APIKey != "test-key" {
		t.Errorf("api_key=%q", c.APIKey)
	}
}

func TestEnvOverride(t *testing.T) {
	t.Setenv("TW2A_LISTEN", ":7777")
	t.Setenv("TW2A_DEFAULT_MODEL", "qwen-3.7-plus")
	t.Setenv("TW2A_SOFT_RATE", "30s")
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":7777" || c.DefaultModel != "qwen-3.7-plus" {
		t.Errorf("c=%+v", c)
	}
	if c.SoftRateDur.Seconds() != 30 {
		t.Errorf("soft_rate=%v", c.SoftRateDur)
	}
}

func TestBadDuration(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "c.json")
	os.WriteFile(fp, []byte(`{"cooldown":{"plan_credit":"not-a-duration"}}`), 0o600)
	if _, err := Load(fp); err == nil {
		t.Fatal("want error for bad duration")
	}
}
