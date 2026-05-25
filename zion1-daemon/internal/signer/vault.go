// vault.go — HashiCorp Vault KV v2에서 attester 개인키 로딩.
//
// Phase 1.5 보안 단계: yaml에 평문 키 저장 ❌ → Vault KV v2 secret에서 fetch ✅.
// 키는 process 메모리에 LocalSigner로 보관 — 한 단계 강화 (디스크 평문 없음).
//
// Phase 2 더 강화: Vault Transit (key 자체가 Vault 밖으로 안 나감, 매 sign이 HTTP call).
// 이 파일은 KV v2 fetch만 구현.
//
// Vault 인증: VAULT_TOKEN 환경 변수 (간단). production은 AppRole / Kubernetes auth.
package signer

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// VaultKVConfig는 Vault KV v2 로딩 설정.
type VaultKVConfig struct {
	// Addr 예: "https://vault.internal:8200"
	Addr string
	// Mount 예: "secret" (KV v2 mount)
	Mount string
	// Path 예: "zion1/attester" — fetch URL이 ${Addr}/v1/${Mount}/data/${Path}
	Path string
	// KeyField 예: "private_key_hex" — secret.data.data[KeyField]에서 키 추출
	KeyField string
	// TokenEnv 비어 있으면 "VAULT_TOKEN"
	TokenEnv string
	// Timeout HTTP request 타임아웃 (default 10s)
	Timeout time.Duration
}

// NewLocalFromVault는 Vault KV v2 secret을 fetch해서 LocalSigner 생성.
// HTTP 호출은 한 번 (startup) — 이후 서명은 LocalSigner 인메모리.
func NewLocalFromVault(cfg VaultKVConfig) (*LocalSigner, error) {
	if cfg.Addr == "" || cfg.Mount == "" || cfg.Path == "" || cfg.KeyField == "" {
		return nil, fmt.Errorf("vault config requires addr/mount/path/key_field")
	}
	tokenEnv := cfg.TokenEnv
	if tokenEnv == "" {
		tokenEnv = "VAULT_TOKEN"
	}
	token := os.Getenv(tokenEnv)
	if token == "" {
		return nil, fmt.Errorf("vault token not set (env %s)", tokenEnv)
	}
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	url := fmt.Sprintf("%s/v1/%s/data/%s",
		strings.TrimRight(cfg.Addr, "/"),
		strings.Trim(cfg.Mount, "/"),
		strings.Trim(cfg.Path, "/"),
	)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("build vault req: %w", err)
	}
	req.Header.Set("X-Vault-Token", token)

	client := &http.Client{Timeout: timeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("vault GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read vault body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("vault returned %d: %s", resp.StatusCode, string(body))
	}

	// Vault KV v2 응답 형식: { "data": { "data": { "<field>": "<value>", ... }, "metadata": {...} } }
	var parsed struct {
		Data struct {
			Data map[string]string `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, fmt.Errorf("parse vault json: %w", err)
	}
	keyHex, ok := parsed.Data.Data[cfg.KeyField]
	if !ok || keyHex == "" {
		return nil, fmt.Errorf("vault secret missing field %q", cfg.KeyField)
	}
	return NewLocal(keyHex)
}
