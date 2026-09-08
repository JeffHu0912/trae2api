package auth

import (
	"regexp"
	"testing"
)

// 16 位纯数字，首位 1-9。
var deviceIDRe = regexp.MustCompile(`^[1-9][0-9]{15}$`)

func TestNewCheckinDeviceIDFormat(t *testing.T) {
	for i := 0; i < 200; i++ {
		id, err := NewCheckinDeviceID()
		if err != nil {
			t.Fatalf("NewCheckinDeviceID error: %v", err)
		}
		if !deviceIDRe.MatchString(id) {
			t.Fatalf("deviceId %q not a 16-digit number with non-zero leading digit", id)
		}
	}
}

func TestNewCheckinDeviceIDUniqueness(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id, err := NewCheckinDeviceID()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if seen[id] {
			t.Fatalf("duplicate deviceId %q", id)
		}
		seen[id] = true
	}
}

func TestEnsureCheckinDeviceID(t *testing.T) {
	// 空 deviceId → 自动填充
	a := &Auth{DeviceID: ""}
	gen, err := a.EnsureCheckinDeviceID()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !gen {
		t.Fatal("expected generation when DeviceID empty")
	}
	if !deviceIDRe.MatchString(a.DeviceID) {
		t.Fatalf("generated deviceId %q invalid", a.DeviceID)
	}

	// 已有 deviceId → 不覆盖
	a2 := &Auth{DeviceID: "2355572504545628"}
	gen2, err := a2.EnsureCheckinDeviceID()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gen2 {
		t.Fatal("should NOT regenerate when DeviceID already present")
	}
	if a2.DeviceID != "2355572504545628" {
		t.Fatalf("existing deviceId was overwritten: %q", a2.DeviceID)
	}
}
