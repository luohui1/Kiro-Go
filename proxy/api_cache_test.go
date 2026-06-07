package proxy

import (
	"encoding/json"
	"kiro-go/config"
	"path/filepath"
	"testing"
	"time"
)

func TestAPICacheKeyIgnoresVolatileKiroFields(t *testing.T) {
	base := &KiroPayload{}
	base.ConversationState.AgentContinuationId = "random-a"
	base.ConversationState.ConversationID = "conversation-a"
	base.ProfileArn = "arn:a"
	base.ConversationState.ChatTriggerType = "MANUAL"
	base.ConversationState.CurrentMessage.UserInputMessage = KiroUserInputMessage{
		Content: "same prompt",
		ModelID: "claude-sonnet-4.5",
		Origin:  "AI_EDITOR",
	}

	other := *base
	other.ConversationState.AgentContinuationId = "random-b"
	other.ConversationState.ConversationID = "conversation-b"
	other.ProfileArn = "arn:b"

	keyA := buildAPICacheKey("openai-chat", base, map[string]interface{}{"model": "claude-sonnet-4.5"})
	keyB := buildAPICacheKey("openai-chat", &other, map[string]interface{}{"model": "claude-sonnet-4.5"})
	if keyA != keyB {
		t.Fatalf("expected volatile fields to be ignored, got %q vs %q", keyA, keyB)
	}
}

func TestAPICacheRoundTripAndExpiry(t *testing.T) {
	if err := config.Init(filepath.Join(t.TempDir(), "config.json")); err != nil {
		t.Fatalf("config init: %v", err)
	}

	key := "test-cache-key"
	data, _ := json.Marshal(map[string]string{"ok": "yes"})
	if err := saveAPICacheEntry(key, data, time.Second); err != nil {
		t.Fatalf("save cache: %v", err)
	}

	got, ok := loadAPICacheEntry(key)
	if !ok {
		t.Fatalf("expected cache hit")
	}
	var gotValue map[string]string
	var wantValue map[string]string
	if err := json.Unmarshal(got, &gotValue); err != nil {
		t.Fatalf("decode cached data: %v", err)
	}
	if err := json.Unmarshal(data, &wantValue); err != nil {
		t.Fatalf("decode expected data: %v", err)
	}
	if gotValue["ok"] != wantValue["ok"] {
		t.Fatalf("expected cached data %v, got %v", wantValue, gotValue)
	}

	if err := saveAPICacheEntry(key, data, time.Nanosecond); err != nil {
		t.Fatalf("save expiring cache: %v", err)
	}
	time.Sleep(time.Millisecond)
	if _, ok := loadAPICacheEntry(key); ok {
		t.Fatalf("expected expired cache miss")
	}
}
