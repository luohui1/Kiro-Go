package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestUpdateSettingsPatchPreservesOmittedAPIKeyFields(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("init config: %v", err)
	}
	if err := UpdateSettings("proxy-api-key", true, "admin-password"); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	if err := UpdateSettingsPatch(nil, nil, "new-admin-password"); err != nil {
		t.Fatalf("patch settings: %v", err)
	}

	if got := GetApiKey(); got != "proxy-api-key" {
		t.Fatalf("expected API key to be preserved, got %q", got)
	}
	if !IsApiKeyRequired() {
		t.Fatalf("expected requireApiKey to stay enabled")
	}
	if got := GetPassword(); got != "new-admin-password" {
		t.Fatalf("expected password to update, got %q", got)
	}
}

func TestUpdateSettingsPatchCanExplicitlyDisableAPIKey(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("init config: %v", err)
	}
	if err := UpdateSettings("proxy-api-key", true, "admin-password"); err != nil {
		t.Fatalf("seed settings: %v", err)
	}

	emptyKey := ""
	requireAPIKey := false
	if err := UpdateSettingsPatch(&emptyKey, &requireAPIKey, ""); err != nil {
		t.Fatalf("patch settings: %v", err)
	}

	if got := GetApiKey(); got != "" {
		t.Fatalf("expected API key to be cleared, got %q", got)
	}
	if IsApiKeyRequired() {
		t.Fatalf("expected requireApiKey to be disabled")
	}
	if got := GetPassword(); got != "admin-password" {
		t.Fatalf("expected password to be preserved, got %q", got)
	}
}

func TestClientModeDefaultsAndAccountOverride(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("init config: %v", err)
	}

	if got := GetClientMode(); got != ClientModeKiroIDE {
		t.Fatalf("expected default client mode kiro-ide, got %q", got)
	}
	if got := EffectiveClientMode(&Account{}); got != ClientModeKiroIDE {
		t.Fatalf("expected empty account to use global kiro-ide, got %q", got)
	}

	if err := UpdateClientMode(ClientModeKiroCLI); err != nil {
		t.Fatalf("update global client mode: %v", err)
	}
	if got := EffectiveClientMode(&Account{}); got != ClientModeKiroCLI {
		t.Fatalf("expected empty account to use global kiro-cli, got %q", got)
	}
	if got := EffectiveClientMode(&Account{ClientMode: ClientModeKiroIDE}); got != ClientModeKiroIDE {
		t.Fatalf("expected account override kiro-ide, got %q", got)
	}
}

// TestAccountAllowOverageMigration verifies that a config.json from before the
// upstream-Overages-switch refactor (which carried `allowOverage: true` per
// account) is migrated into OverageStatus="ENABLED" on first load, and that
// the legacy field is cleared so future saves don't re-emit it.
func TestAccountAllowOverageMigration(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")

	seed := map[string]interface{}{
		"password":      "p",
		"port":          8080,
		"host":          "0.0.0.0",
		"requireApiKey": false,
		"accounts": []map[string]interface{}{
			{"id": "acc-allow", "enabled": true, "allowOverage": true},
			{"id": "acc-deny", "enabled": true, "allowOverage": false},
			{"id": "acc-already-set", "enabled": true, "allowOverage": true, "overageStatus": "DISABLED"},
		},
	}
	raw, err := json.MarshalIndent(seed, "", "  ")
	if err != nil {
		t.Fatalf("marshal seed: %v", err)
	}
	if err := os.WriteFile(cfgFile, raw, 0600); err != nil {
		t.Fatalf("write seed: %v", err)
	}

	if err := Init(cfgFile); err != nil {
		t.Fatalf("init: %v", err)
	}

	accounts := GetAccounts()
	byID := map[string]Account{}
	for _, a := range accounts {
		byID[a.ID] = a
	}

	if got := byID["acc-allow"].OverageStatus; got != "ENABLED" {
		t.Fatalf("expected acc-allow to migrate to OverageStatus=ENABLED, got %q", got)
	}
	if byID["acc-allow"].LegacyAllowOverage {
		t.Fatalf("expected legacy allowOverage to be cleared after migration")
	}
	if got := byID["acc-deny"].OverageStatus; got != "" {
		t.Fatalf("expected acc-deny to keep empty OverageStatus, got %q", got)
	}
	// Pre-set OverageStatus must win over the legacy field.
	if got := byID["acc-already-set"].OverageStatus; got != "DISABLED" {
		t.Fatalf("expected acc-already-set OverageStatus to be preserved, got %q", got)
	}
	if byID["acc-already-set"].LegacyAllowOverage {
		t.Fatalf("expected legacy field to still be cleared on acc-already-set")
	}

	// Re-read the file and confirm legacy field is gone (so it doesn't drift
	// back in on later saves).
	on_disk, err := os.ReadFile(cfgFile)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var reloaded struct {
		Accounts []map[string]interface{} `json:"accounts"`
	}
	if err := json.Unmarshal(on_disk, &reloaded); err != nil {
		t.Fatalf("decode reload: %v", err)
	}
	for _, a := range reloaded.Accounts {
		if _, ok := a["allowOverage"]; ok {
			t.Fatalf("expected allowOverage to be omitted from persisted file, got %+v", a)
		}
	}
}

func TestEndpoint429RetryKnobDefaultsAndDisable(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("init config: %v", err)
	}

	// Unset (0) falls back to the safe defaults.
	if got := GetEndpoint429RetryIntervalMs(); got != 2000 {
		t.Fatalf("expected default interval 2000, got %d", got)
	}
	if got := GetEndpoint429RetryMaxWaitMs(); got != 25000 {
		t.Fatalf("expected default max-wait 25000, got %d", got)
	}

	// A positive interval is used verbatim.
	if err := UpdateEndpoint429RetryIntervalMs(1500); err != nil {
		t.Fatalf("update interval: %v", err)
	}
	if got := GetEndpoint429RetryIntervalMs(); got != 1500 {
		t.Fatalf("expected interval 1500, got %d", got)
	}

	// -1 disables retry: the getter reports 0 so the caller skips the retry loop.
	if err := UpdateEndpoint429RetryIntervalMs(-1); err != nil {
		t.Fatalf("disable interval: %v", err)
	}
	if got := GetEndpoint429RetryIntervalMs(); got != 0 {
		t.Fatalf("expected disabled interval to report 0, got %d", got)
	}

	// Negative max-wait is normalized to 0, which the getter maps to the default.
	if err := UpdateEndpoint429RetryMaxWaitMs(-5); err != nil {
		t.Fatalf("update max-wait: %v", err)
	}
	if got := GetEndpoint429RetryMaxWaitMs(); got != 25000 {
		t.Fatalf("expected normalized max-wait to fall back to 25000, got %d", got)
	}
}

func TestPromptCacheConfigDefaultsAndUpdate(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("init config: %v", err)
	}

	cfg := GetPromptCacheConfig()
	if !cfg.AccountingEnabled {
		t.Fatalf("expected prompt cache accounting to default enabled")
	}
	if cfg.TTLSeconds != 300 {
		t.Fatalf("expected default prompt cache ttl 300, got %d", cfg.TTLSeconds)
	}

	enabled := false
	ttl := 3600
	if err := UpdatePromptCacheConfig(&enabled, &ttl); err != nil {
		t.Fatalf("update prompt cache config: %v", err)
	}

	cfg = GetPromptCacheConfig()
	if cfg.AccountingEnabled {
		t.Fatalf("expected prompt cache accounting to be disabled")
	}
	if cfg.TTLSeconds != 3600 {
		t.Fatalf("expected prompt cache ttl 3600, got %d", cfg.TTLSeconds)
	}
}

func TestAPICacheConfigDefaultsAndUpdate(t *testing.T) {
	if err := Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("init config: %v", err)
	}

	cfg := GetAPICacheConfig()
	if cfg.Enabled {
		t.Fatalf("expected api cache to default disabled")
	}
	if cfg.TTLSeconds != 3600 {
		t.Fatalf("expected default api cache ttl 3600, got %d", cfg.TTLSeconds)
	}
	if cfg.TargetHitPercent != 90 {
		t.Fatalf("expected default target hit percent 90, got %d", cfg.TargetHitPercent)
	}

	enabled := true
	ttl := 7200
	target := 95
	if err := UpdateAPICacheConfig(&enabled, &ttl, &target); err != nil {
		t.Fatalf("update api cache config: %v", err)
	}

	cfg = GetAPICacheConfig()
	if !cfg.Enabled {
		t.Fatalf("expected api cache to be enabled")
	}
	if cfg.TTLSeconds != 7200 {
		t.Fatalf("expected api cache ttl 7200, got %d", cfg.TTLSeconds)
	}
	if cfg.TargetHitPercent != 95 {
		t.Fatalf("expected target hit percent 95, got %d", cfg.TargetHitPercent)
	}
}
