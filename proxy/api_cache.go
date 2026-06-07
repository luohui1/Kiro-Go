package proxy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"kiro-go/config"
	"kiro-go/logger"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const apiCacheDirName = "api-cache"

type apiCacheDoc struct {
	Key       string          `json:"key"`
	StoredAt  int64           `json:"stored_at"`
	ExpiresAt int64           `json:"expires_at"`
	Response  json.RawMessage `json:"response"`
}

func apiCacheDir() string {
	return filepath.Join(config.GetConfigDir(), apiCacheDirName)
}

func buildAPICacheKey(kind string, payload *KiroPayload, options map[string]interface{}) string {
	value := map[string]interface{}{
		"kind":    kind,
		"payload": normalizeAPICachePayload(payload),
		"options": options,
	}
	sum := sha256.Sum256([]byte(canonicalizeCacheValue(value)))
	return hex.EncodeToString(sum[:])
}

func normalizeAPICachePayload(payload *KiroPayload) interface{} {
	if payload == nil {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return payload
	}
	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return payload
	}
	stripAPICacheVolatileFields(value)
	return value
}

func stripAPICacheVolatileFields(value interface{}) {
	switch v := value.(type) {
	case map[string]interface{}:
		delete(v, "profileArn")
		for key, item := range v {
			if isAPICacheVolatileKey(key) {
				delete(v, key)
				continue
			}
			stripAPICacheVolatileFields(item)
		}
	case []interface{}:
		for _, item := range v {
			stripAPICacheVolatileFields(item)
		}
	}
}

func isAPICacheVolatileKey(key string) bool {
	switch key {
	case "agentContinuationId", "conversationId":
		return true
	default:
		return false
	}
}

func loadAPICacheEntry(key string) ([]byte, bool) {
	path := filepath.Join(apiCacheDir(), sanitizeAPICacheKey(key)+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var doc apiCacheDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		_ = os.Remove(path)
		return nil, false
	}
	if doc.ExpiresAt > 0 && time.Now().Unix() >= doc.ExpiresAt {
		_ = os.Remove(path)
		return nil, false
	}
	if len(doc.Response) == 0 {
		return nil, false
	}
	return append([]byte(nil), doc.Response...), true
}

func saveAPICacheEntry(key string, response []byte, ttl time.Duration) error {
	if strings.TrimSpace(key) == "" || len(response) == 0 || ttl <= 0 {
		return nil
	}
	dir := apiCacheDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create api cache dir: %w", err)
	}
	now := time.Now()
	doc := apiCacheDoc{
		Key:       key,
		StoredAt:  now.Unix(),
		ExpiresAt: now.Add(ttl).Unix(),
		Response:  append(json.RawMessage(nil), response...),
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal api cache: %w", err)
	}
	path := filepath.Join(dir, sanitizeAPICacheKey(key)+".json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write api cache: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("commit api cache: %w", err)
	}
	return nil
}

func writeAPICacheHit(w http.ResponseWriter, data []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Kiro-Cache", "hit")
	_, _ = w.Write(data)
}

func maybeSaveAPICache(key string, response []byte) {
	cfg := config.GetAPICacheConfig()
	if !cfg.Enabled {
		return
	}
	if !shouldAdmitAPICacheKey(key, cfg.TargetHitPercent) {
		return
	}
	if err := saveAPICacheEntry(key, response, time.Duration(cfg.TTLSeconds)*time.Second); err != nil {
		logger.Warnf("[APICache] save %s failed: %v", key, err)
	}
}

func shouldAdmitAPICacheKey(key string, targetHitPercent int) bool {
	if targetHitPercent <= 0 {
		targetHitPercent = 90
	}
	if targetHitPercent > 100 {
		targetHitPercent = 100
	}
	if targetHitPercent >= 100 {
		return true
	}
	sum := sha256.Sum256([]byte(key))
	bucket := ((int(sum[0]) << 8) | int(sum[1])) % 100
	return bucket < targetHitPercent
}

func loadAPICacheIfEnabled(key string) ([]byte, bool) {
	cfg := config.GetAPICacheConfig()
	if !cfg.Enabled {
		return nil, false
	}
	return loadAPICacheEntry(key)
}

func sanitizeAPICacheKey(key string) string {
	cleaned := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		default:
			return -1
		}
	}, key)
	if cleaned == "" {
		return "invalid"
	}
	return cleaned
}
