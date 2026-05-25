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

type AttesterConfig struct {
	PrivateKeyHex string `yaml:"private_key_hex"`
	EVMAddress    string `yaml:"evm_address"`
}

type DaemonConfig struct {
	StatePath           string `yaml:"state_path"`
	PollIntervalSeconds int    `yaml:"poll_interval_seconds"`
	SignatureTTLSeconds int64  `yaml:"signature_ttl_seconds"`
	MaxRetries          int    `yaml:"max_retries"`
	RetryBackoffSeconds int    `yaml:"retry_backoff_seconds"`
	LogLevel            string `yaml:"log_level"`
}

type HandlersConfig struct {
	PoPAttest   bool `yaml:"pop_attest"`
	BridgeMint  bool `yaml:"bridge_mint"`
	JobAttest   bool `yaml:"job_attest"`
	DayAttest   bool `yaml:"day_attest"`
}

type MockConfig struct {
	Enabled       bool   `yaml:"enabled"`
	FixturesPath  string `yaml:"fixtures_path"`
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
	if c.Attester.PrivateKeyHex == "" || len(c.Attester.PrivateKeyHex) != 64 {
		return fmt.Errorf("attester.private_key_hex must be 64 hex chars")
	}
	if c.Daemon.SignatureTTLSeconds <= 0 {
		return fmt.Errorf("daemon.signature_ttl_seconds must be > 0")
	}
	return nil
}
