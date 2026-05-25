// Command zion1-daemon은 SeumStandard 브리지 데몬.
//
// 사용:
//   zion1-daemon -config config.yaml
//
// 동작: Zion CometBFT WebSocket 구독 → 이벤트 디코딩 → EIP-191 서명 →
//      SeumStandard EVM 컨트랙트 트랜잭션 송출.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/okneo31/zion1-daemon/internal/config"
	"github.com/okneo31/zion1-daemon/internal/evmclient"
	"github.com/okneo31/zion1-daemon/internal/handlers"
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

	// 시그너
	sgn, err := signer.New(cfg.Attester.PrivateKeyHex)
	if err != nil {
		logger.Error("signer init", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("attester loaded", "addr", sgn.Address().Hex())

	// EVM 클라이언트
	evm, err := evmclient.New(
		cfg.EVM.RPCURL,
		cfg.EVM.ChainID,
		cfg.EVM.ContractAddress,
		cfg.EVM.GasLimit,
		cfg.EVM.GasPriceGwei,
		cfg.Attester.PrivateKeyHex, // sender key 같음 (선택적으로 분리 가능)
	)
	if err != nil {
		logger.Error("evm init", "error", err.Error())
		os.Exit(1)
	}
	defer evm.Close()
	logger.Info("evm connected", "contract", evm.Address().Hex(), "chain_id", evm.ChainID())

	// 영구 store
	st, err := store.NewMemoryStore(cfg.Daemon.StatePath)
	if err != nil {
		logger.Error("store init", "error", err.Error())
		os.Exit(1)
	}

	// Dispatcher + 핸들러 등록
	dispatcher := handlers.NewDispatcher(st)
	hc := handlers.HandlerContext{
		Signer:              sgn,
		EVMClient:           evm,
		Store:               st,
		SignatureTTLSeconds: cfg.Daemon.SignatureTTLSeconds,
	}
	if cfg.Handlers.PoPAttest {
		dispatcher.Register(handlers.NewAttestPoPHandler(hc))
		logger.Info("handler registered", "type", "pop_attest")
	}
	// TODO: bridge_mint, job_attest, day_attest 핸들러 (Phase 2)
	if cfg.Handlers.BridgeMint {
		logger.Warn("handler bridge_mint not yet implemented")
	}

	// 컨텍스트 + 시그널 처리
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		logger.Info("shutdown signal received")
		cancel()
	}()

	// 이벤트 소스 (mock 또는 실제 Zion)
	var source <-chan zionclient.TypedEvent
	if cfg.Mock.Enabled {
		// TODO: fixture loading
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
		// 모든 SeumStandard 관련 이벤트 구독 (단일 query로)
		query := "tm.event='Tx' AND zion.seum.v1.EventEvmMappingRegistered.evm_addr EXISTS"
		s, err := zc.Subscribe(ctx, "zion1-daemon", query)
		if err != nil {
			logger.Error("subscribe", "error", err.Error())
			os.Exit(1)
		}
		source = s
		logger.Info("zion subscribed", "query", query)
	}

	// 파이프라인 실행
	p := pipeline.New(source, dispatcher, cfg.Daemon.MaxRetries, cfg.Daemon.RetryBackoffSeconds, logger)
	logger.Info("daemon started")
	if err := p.Run(ctx); err != nil && err != context.Canceled {
		logger.Error("pipeline exit", "error", err.Error())
		os.Exit(1)
	}
	logger.Info("daemon stopped")
}
