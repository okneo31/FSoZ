// Command zion1-daemon은 SeumStandard 브리지 데몬.
//
// 사용:
//
//	zion1-daemon -config config.yaml
//
// 동작 (양방향):
//
//	Zion → EVM:  CometBFT WS 구독 → 이벤트 디코딩 → EIP-191 서명 → SeumStandard tx 송출
//	EVM → Zion:  RagequitRequest 감시 → (TODO: Zion x/seum.MsgUnlockCapital broadcast)
package main

import (
	"context"
	"flag"
	"log/slog"
	"math/big"
	"os"
	"os/signal"
	"syscall"

	"github.com/okneo31/zion1-daemon/internal/config"
	"github.com/okneo31/zion1-daemon/internal/evmclient"
	"github.com/okneo31/zion1-daemon/internal/handlers"
	"github.com/okneo31/zion1-daemon/internal/metrics"
	"github.com/okneo31/zion1-daemon/internal/pipeline"
	"github.com/okneo31/zion1-daemon/internal/signer"
	"github.com/okneo31/zion1-daemon/internal/store"
	"github.com/okneo31/zion1-daemon/internal/zionclient"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config yaml")
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Error("config load", "error", err.Error())
		os.Exit(1)
	}

	// ─── 시그너 (plaintext or vault) ───
	sgn, err := loadSigner(cfg)
	if err != nil {
		logger.Error("signer init", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("attester loaded", "addr", sgn.Address().Hex(), "source", cfg.AttesterSourceOrDefault())

	// ─── EVM 클라이언트 ───
	// tx 송신 키는 attester 키 재사용 (선택적으로 분리 가능). vault 모드면 attester가 LocalSigner라 PrivateKey() 가능.
	evm, err := evmclient.New(
		cfg.EVM.RPCURL,
		cfg.EVM.ChainID,
		cfg.EVM.ContractAddress,
		cfg.EVM.GasLimit,
		cfg.EVM.GasPriceGwei,
		exportSenderKeyHex(sgn),
	)
	if err != nil {
		logger.Error("evm init", "error", err.Error())
		os.Exit(1)
	}
	defer evm.Close()
	logger.Info("evm connected", "contract", evm.Address().Hex(), "chain_id", evm.ChainID(), "sender", evm.SenderAddress().Hex())

	// ─── 영구 store ───
	st, closeStore, err := openStore(cfg)
	if err != nil {
		logger.Error("store init", "error", err.Error())
		os.Exit(1)
	}
	defer closeStore()
	logger.Info("store opened", "type", cfg.StateTypeOrDefault(), "path", cfg.Daemon.StatePath)

	// ─── 메트릭 ───
	var prom *metrics.PromMetrics
	if cfg.Metrics.Enabled {
		prom = metrics.New()
	}
	var metricsForHandlers handlers.Metrics
	if prom != nil {
		metricsForHandlers = prom
	}

	// ─── Dispatcher + 핸들러 등록 ───
	dispatcher := handlers.NewDispatcher(st)
	hc := handlers.HandlerContext{
		Signer:              sgn,
		EVMClient:           evm,
		Store:               st,
		SignatureTTLSeconds: cfg.Daemon.SignatureTTLSeconds,
		Metrics:             metricsForHandlers,
	}
	if cfg.Handlers.PoPAttest {
		dispatcher.Register(handlers.NewAttestPoPHandler(hc))
		logger.Info("handler registered", "type", "pop_attest")
	}
	if cfg.Handlers.BridgeMint {
		dispatcher.Register(handlers.NewBridgeMintHandler(hc))
		logger.Info("handler registered", "type", "bridge_mint")
	}
	if cfg.Handlers.HonorAttest {
		dispatcher.Register(handlers.NewAttestHonorHandler(hc))
		logger.Info("handler registered", "type", "honor_attest")
	}
	if cfg.Handlers.DayAttest {
		dispatcher.Register(handlers.NewAttestDayHandler(hc))
		logger.Info("handler registered", "type", "day_attest")
	}

	// ─── 컨텍스트 + 시그널 ───
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("shutdown signal received")
		cancel()
	}()

	// ─── 메트릭 HTTP 서버 (선택) ───
	if prom != nil {
		go func() {
			logger.Info("metrics server starting", "addr", cfg.Metrics.Addr)
			if err := prom.Serve(ctx, cfg.Metrics.Addr); err != nil && err != context.Canceled {
				logger.Error("metrics server error", "error", err.Error())
			}
		}()
	}

	// ─── 이벤트 소스 (mock 또는 실제 Zion) ───
	var source <-chan zionclient.TypedEvent
	if cfg.Mock.Enabled {
		mock := zionclient.NewMockSource(nil)
		source = mock.Stream(ctx)
		logger.Info("running in mock mode", "fixtures", cfg.Mock.FixturesPath)
	} else {
		zc, err := zionclient.New(cfg.Zion.RPCURL, cfg.Zion.ChainID)
		if err != nil {
			logger.Error("zion client init", "error", err.Error())
			os.Exit(1)
		}
		defer zc.Close()
		// 단일 query — 모든 SeumStandard 관련 이벤트
		query := "tm.event='Tx' AND (" +
			"zion.seum.v1.EventEvmMappingRegistered.evm_addr EXISTS OR " +
			"zion.seum.v1.EventCapitalLocked.evm_addr EXISTS OR " +
			"zion.seum.v1.EventHonorAttested.evm_addr EXISTS OR " +
			"zion.seum.v1.EventDayAttested.evm_addr EXISTS)"
		s, err := zc.Subscribe(ctx, "zion1-daemon", query)
		if err != nil {
			logger.Error("subscribe", "error", err.Error())
			os.Exit(1)
		}
		source = s
		logger.Info("zion subscribed", "query", query)
	}

	// ─── EVM Ragequit 감시 (역방향) ───
	if cfg.Handlers.EVMRagequit {
		ragequitCh := make(chan evmclient.RagequitEvent, 16)
		// Phase 1.5: logging-only broadcaster (cosmos-sdk client 통합은 Phase 2).
		// 명시적 타입으로 "의도적 stub" 임을 표시.
		zionBroadcaster := handlers.NewLoggingBroadcaster(logger, metricsForHandlers)
		rqHandler := handlers.NewEVMRagequitHandler(st, zionBroadcaster, metricsForHandlers)
		go func() {
			if err := evm.WatchRagequit(ctx, nil /* fromBlock: head */, ragequitCh); err != nil && err != context.Canceled {
				logger.Error("ragequit watch error", "error", err.Error())
			}
		}()
		go func() {
			if err := rqHandler.Run(ctx, ragequitCh); err != nil && err != context.Canceled {
				logger.Error("ragequit handler error", "error", err.Error())
			}
		}()
		logger.Info("evm ragequit watcher started")
	}

	// ─── 파이프라인 실행 (메인 루프) ───
	p := pipeline.New(source, dispatcher, cfg.Daemon.MaxRetries, cfg.Daemon.RetryBackoffSeconds, logger)
	logger.Info("daemon started")
	if err := p.Run(ctx); err != nil && err != context.Canceled {
		logger.Error("pipeline exit", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("daemon stopped")
}

// loadSigner는 config source에 따라 LocalSigner 또는 Vault-loaded LocalSigner 반환.
func loadSigner(cfg *config.Config) (*signer.LocalSigner, error) {
	switch cfg.AttesterSourceOrDefault() {
	case "vault":
		return signer.NewLocalFromVault(signer.VaultKVConfig{
			Addr:     cfg.Attester.Vault.Addr,
			Mount:    cfg.Attester.Vault.Mount,
			Path:     cfg.Attester.Vault.Path,
			KeyField: cfg.Attester.Vault.KeyField,
			TokenEnv: cfg.Attester.Vault.TokenEnv,
		})
	default: // "plaintext"
		return signer.NewLocal(cfg.Attester.PrivateKeyHex)
	}
}

// openStore는 config에 따라 MemoryStore 또는 BoltStore 반환 + close 함수.
func openStore(cfg *config.Config) (store.Store, func(), error) {
	switch cfg.StateTypeOrDefault() {
	case "bolt":
		s, err := store.NewBoltStore(cfg.Daemon.StatePath)
		if err != nil {
			return nil, nil, err
		}
		return s, func() { _ = s.Close() }, nil
	default: // "memory"
		s, err := store.NewMemoryStore(cfg.Daemon.StatePath)
		if err != nil {
			return nil, nil, err
		}
		return s, func() {}, nil
	}
}

// exportSenderKeyHex extracts the private key hex from LocalSigner for tx-signing reuse.
// LocalSigner exposes PrivateKey(); vault-loaded signer is also LocalSigner, so same path works.
func exportSenderKeyHex(sgn *signer.LocalSigner) string {
	pk := sgn.PrivateKey()
	b := pk.D.Bytes()
	// pad to 32 bytes
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return hexEncode(out)
}

func hexEncode(b []byte) string {
	const hexchars = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, x := range b {
		out[i*2] = hexchars[x>>4]
		out[i*2+1] = hexchars[x&0xf]
	}
	return string(out)
}

// 미사용 import 방지
var _ = big.NewInt
