// Package config는 zion1.top 데몬의 설정 로딩.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Zion     ZionConfig     `yaml:"zion"`
	EVM      EVMConfig      `yaml:"evm"`
	Attester AttesterConfig `yaml:"attester"`
	Daemon   DaemonConfig   `yaml:"daemon"`
	Handlers HandlersConfig `yaml:"handlers"`
	Mock     MockConfig     `yaml:"mock"`
	Metrics  MetricsConfig  `yaml:"metrics"`
}

type ZionConfig struct {
	RPCURL  string `yaml:"rpc_url"`
	HTTPURL string `yaml:"http_url"`
	GRPCURL string `yaml:"grpc_url"`
	ChainID string `yaml:"chain_id"`
}

type EVMConfig struct {
	RPCURL          string `yaml:"rpc_url"`
	ChainID         int64  `yaml:"chain_id"`
	ContractAddress string `yaml:"contract_address"`
	GasLimit        uint64 `yaml:"gas_limit"`
	GasPriceGwei    int64  `yaml:"gas_price_gwei"`
}

// AttesterConfig는 키 source 선택 가능 — plaintext / vault.
//
//	source: "plaintext"  → private_key_hex 사용 (개발/테스트)
//	source: "vault"      → vault.* 필드에서 KV v2 fetch (Phase 1.5)
type AttesterConfig struct {
	Source        string      `yaml:"source"`         // "plaintext" | "vault"
	PrivateKeyHex string      `yaml:"private_key_hex"` // plaintext용
	EVMAddress    string      `yaml:"evm_address"`    // 공개 (검증용)
	Vault         VaultConfig `yaml:"vault"`
}

type VaultConfig struct {
	Addr     string `yaml:"addr"`      // 예: "https://vault.internal:8200"
	Mount    string `yaml:"mount"`     // 예: "secret"
	Path     string `yaml:"path"`      // 예: "zion1/attester"
	KeyField string `yaml:"key_field"` // 예: "private_key_hex"
	TokenEnv string `yaml:"token_env"` // 비어 있으면 "VAULT_TOKEN"
}

type DaemonConfig struct {
	StateType           string `yaml:"state_type"`            // "memory" | "bolt"
	StatePath           string `yaml:"state_path"`            // memory=json snapshot path, bolt=db file
	PollIntervalSeconds int    `yaml:"poll_interval_seconds"`
	SignatureTTLSeconds int64  `yaml:"signature_ttl_seconds"`
	MaxRetries          int    `yaml:"max_retries"`
	RetryBackoffSeconds int    `yaml:"retry_backoff_seconds"`
	LogLevel            string `yaml:"log_level"`
}

type HandlersConfig struct {
	PoPAttest    bool `yaml:"pop_attest"`
	BridgeMint   bool `yaml:"bridge_mint"`
	HonorAttest  bool `yaml:"honor_attest"`
	DayAttest    bool `yaml:"day_attest"`
	EVMRagequit  bool `yaml:"evm_ragequit"` // EVM RagequitRequest 감시 + Zion unlock
}

type MockConfig struct {
	Enabled      bool   `yaml:"enabled"`
	FixturesPath string `yaml:"fixtures_path"`
}

type MetricsConfig struct {
	Enabled bool   `yaml:"enabled"`
	Addr    string `yaml:"addr"` // 예: ":9100"
}

// Load는 yaml 파일에서 설정 로딩.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return &cfg, nil
}

// Validate는 필수 필드 검증.
func (c *Config) Validate() error {
	if c.Zion.ChainID == "" {
		return fmt.Errorf("zion.chain_id required")
	}
	if c.EVM.RPCURL == "" {
		return fmt.Errorf("evm.rpc_url required")
	}
	if c.EVM.ContractAddress == "" || c.EVM.ContractAddress == "0x0000000000000000000000000000000000000000" {
		return fmt.Errorf("evm.contract_address required (not zero)")
	}

	// attester source 검증
	source := c.Attester.Source
	if source == "" {
		source = "plaintext" // backwards compatible default
	}
	switch source {
	case "plaintext":
		if c.Attester.PrivateKeyHex == "" || len(c.Attester.PrivateKeyHex) != 64 {
			return fmt.Errorf("attester.private_key_hex must be 64 hex chars (plaintext source)")
		}
	case "vault":
		if c.Attester.Vault.Addr == "" || c.Attester.Vault.Mount == "" || c.Attester.Vault.Path == "" || c.Attester.Vault.KeyField == "" {
			return fmt.Errorf("attester.vault.{addr,mount,path,key_field} required")
		}
	default:
		return fmt.Errorf("attester.source must be 'plaintext' or 'vault', got %q", source)
	}

	if c.Daemon.SignatureTTLSeconds <= 0 {
		return fmt.Errorf("daemon.signature_ttl_seconds must be > 0")
	}
	if c.Daemon.StateType != "" && c.Daemon.StateType != "memory" && c.Daemon.StateType != "bolt" {
		return fmt.Errorf("daemon.state_type must be 'memory' or 'bolt'")
	}
	if c.Metrics.Enabled && c.Metrics.Addr == "" {
		return fmt.Errorf("metrics.addr required when metrics.enabled=true")
	}
	return nil
}

// AttesterSourceOrDefault returns the effective source (defaults to plaintext).
func (c *Config) AttesterSourceOrDefault() string {
	if c.Attester.Source == "" {
		return "plaintext"
	}
	return c.Attester.Source
}

// StateTypeOrDefault returns the effective state type (defaults to memory).
func (c *Config) StateTypeOrDefault() string {
	if c.Daemon.StateType == "" {
		return "memory"
	}
	return c.Daemon.StateType
}
