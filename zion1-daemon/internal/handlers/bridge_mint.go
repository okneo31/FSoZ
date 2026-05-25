// bridge_mint는 Zion x/seum.EventCapitalLocked → EVM bridgeMintCapital 호출.
//
// Zion 측 흐름 (참조 — 이 데몬 밖):
//
//	user → MsgLockCapitalForSeum(amount utrg) → x/seum 모듈이 escrow + lockId 발급 + 이벤트 emit
//
// 이 핸들러:
//
//	EventCapitalLocked{evm_addr, kwr_amount, lock_id} → digest 서명 → EVM bridgeMintCapital 호출
//
// EVM 측은 lockedKWR += kwr_amount, baseHonor[CAPITAL] += kwr_amount.
// lockId가 consumed로 mark되어 replay 방지. ragequit 시 unlock은 역방향 (evm_ragequit.go).
package handlers

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/okneo31/zion1-daemon/internal/signer"
	"github.com/okneo31/zion1-daemon/internal/zionclient"
)

const EventCapitalLocked = "zion.seum.v1.EventCapitalLocked"

type BridgeMintHandler struct {
	hc HandlerContext
}

func NewBridgeMintHandler(hc HandlerContext) *BridgeMintHandler {
	return &BridgeMintHandler{hc: hc}
}

func (h *BridgeMintHandler) EventTypes() []string {
	return []string{EventCapitalLocked}
}

func (h *BridgeMintHandler) Handle(ctx context.Context, ev zionclient.TypedEvent) error {
	metrics := MetricsOrNoop(h.hc.Metrics)

	worker, err := requireAddrAttr(ev, "evm_addr")
	if err != nil {
		metrics.IncEventProcessed("bridge_mint", "bad_addr")
		return err
	}
	amount, err := requireUintAttr(ev, "kwr_amount")
	if err != nil {
		metrics.IncEventProcessed("bridge_mint", "bad_amount")
		return err
	}
	if amount.Sign() <= 0 {
		metrics.IncEventProcessed("bridge_mint", "zero_amount")
		return fmt.Errorf("kwr_amount must be > 0")
	}
	lockID, err := requireBytes32Attr(ev, "lock_id")
	if err != nil {
		metrics.IncEventProcessed("bridge_mint", "bad_lock_id")
		return err
	}

	expiry := uint64(time.Now().Unix() + h.hc.SignatureTTLSeconds)
	signStart := time.Now()
	digest, err := signer.PackBridgeMintCapitalDigest(h.hc.EVMClient.Address(), worker, amount, lockID, expiry)
	if err != nil {
		metrics.IncEventProcessed("bridge_mint", "pack_err")
		return fmt.Errorf("pack digest: %w", err)
	}
	v, r, s, err := h.hc.Signer.SignDigest(digest)
	if err != nil {
		metrics.IncEventProcessed("bridge_mint", "sign_err")
		return fmt.Errorf("sign: %w", err)
	}
	metrics.ObserveSigningDuration("bridge_mint", time.Since(signStart).Seconds())

	_, err = h.hc.EVMClient.BroadcastBridgeMintCapital(ctx, worker, amount, lockID, expiry, v, r, s)
	if err != nil {
		metrics.IncEventProcessed("bridge_mint", "broadcast_err")
		return fmt.Errorf("broadcast bridgeMintCapital: %w", err)
	}
	metrics.IncEventProcessed("bridge_mint", "ok")
	return nil
}

// ─── 공통 attribute 추출 helper ───

func requireAddrAttr(ev zionclient.TypedEvent, key string) (common.Address, error) {
	s, ok := ev.Attributes[key]
	if !ok {
		return common.Address{}, fmt.Errorf("missing %s attribute", key)
	}
	addr := common.HexToAddress(s)
	if (addr == common.Address{}) {
		return common.Address{}, fmt.Errorf("%s is zero address", key)
	}
	return addr, nil
}

func requireUintAttr(ev zionclient.TypedEvent, key string) (*big.Int, error) {
	s, ok := ev.Attributes[key]
	if !ok {
		return nil, fmt.Errorf("missing %s attribute", key)
	}
	n, ok := new(big.Int).SetString(s, 10)
	if !ok {
		return nil, fmt.Errorf("%s not decimal int: %q", key, s)
	}
	return n, nil
}

func requireBytes32Attr(ev zionclient.TypedEvent, key string) ([32]byte, error) {
	var out [32]byte
	s, ok := ev.Attributes[key]
	if !ok {
		return out, fmt.Errorf("missing %s attribute", key)
	}
	// hex bytes32 (with or without 0x)
	if len(s) >= 2 && s[:2] == "0x" {
		s = s[2:]
	}
	if len(s) != 64 {
		return out, fmt.Errorf("%s not 32 bytes hex: len=%d", key, len(s))
	}
	b, err := hexDecodeFixed32(s)
	if err != nil {
		return out, fmt.Errorf("%s hex decode: %w", key, err)
	}
	return b, nil
}

func hexDecodeFixed32(s string) ([32]byte, error) {
	var out [32]byte
	for i := 0; i < 32; i++ {
		hi, err := hexNibble(s[2*i])
		if err != nil {
			return out, err
		}
		lo, err := hexNibble(s[2*i+1])
		if err != nil {
			return out, err
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}

func hexNibble(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	}
	return 0, fmt.Errorf("invalid hex char %q", c)
}

func requireUint64Attr(ev zionclient.TypedEvent, key string) (uint64, error) {
	n, err := requireUintAttr(ev, key)
	if err != nil {
		return 0, err
	}
	if !n.IsUint64() {
		return 0, fmt.Errorf("%s overflows uint64: %s", key, n.String())
	}
	return n.Uint64(), nil
}

func requireBoolAttr(ev zionclient.TypedEvent, key string) (bool, error) {
	s, ok := ev.Attributes[key]
	if !ok {
		return false, fmt.Errorf("missing %s attribute", key)
	}
	switch s {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	}
	return false, fmt.Errorf("%s not bool: %q", key, s)
}
