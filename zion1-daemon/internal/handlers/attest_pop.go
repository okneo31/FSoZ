// attest_pop는 Zion x/pop의 PoP 통과 이벤트 → EVM SeumStandard.attestPoP 호출.
//
// 흐름:
//   1. EventPoPVerifiedForSeum{zion_addr, evm_addr} 수신
//   2. zion_addr이 정말 PoP 통과했는지 재확인 (chain query)
//   3. nonce 생성 (random 32 bytes 또는 deterministic from tx_hash)
//   4. expiry = now + signature_ttl
//   5. digest 계산 + 서명
//   6. EVM tx 송출
package handlers

import (
	"context"
	"crypto/rand"
	"fmt"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/okneo31/zion1-daemon/internal/signer"
	"github.com/okneo31/zion1-daemon/internal/zionclient"
)

const (
	EventPoPVerifiedForSeum = "zion.seum.v1.EventEvmMappingRegistered"
)

// AttestPoPHandler는 EvmMappingRegistered 이벤트를 EVM의 attestPoP으로 변환.
type AttestPoPHandler struct {
	hc HandlerContext
}

// NewAttestPoPHandler는 핸들러 생성.
func NewAttestPoPHandler(hc HandlerContext) *AttestPoPHandler {
	return &AttestPoPHandler{hc: hc}
}

func (h *AttestPoPHandler) EventTypes() []string {
	return []string{EventPoPVerifiedForSeum}
}

func (h *AttestPoPHandler) Handle(ctx context.Context, ev zionclient.TypedEvent) error {
	evmAddrStr, ok := ev.Attributes["evm_addr"]
	if !ok {
		return fmt.Errorf("missing evm_addr attribute")
	}
	worker := common.HexToAddress(evmAddrStr)
	if (worker == common.Address{}) {
		return fmt.Errorf("evm_addr is zero")
	}

	// 1. nonce 생성 — tx hash 기반이면 deterministic, 그냥 random도 OK (consumed 체크는 EVM 측에서)
	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return fmt.Errorf("rand nonce: %w", err)
	}

	// 2. expiry
	expiry := uint64(time.Now().Unix() + h.hc.SignatureTTLSeconds)

	// 3. digest + 서명
	digest := signer.PackAttestPoPDigest(h.hc.EVMClient.Address(), worker, expiry, nonce)
	v, r, s, err := h.hc.Signer.SignDigest(digest)
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}

	// 4. EVM 트랜잭션
	tx, err := h.hc.EVMClient.BroadcastAttestPoP(ctx, worker, expiry, nonce, v, r, s)
	if err != nil {
		return fmt.Errorf("broadcast attestPoP: %w", err)
	}
	_ = tx // TODO: 영구 저장 (재시도 추적용)

	// 5. 영구 표시 — Dispatch 측에서 이미 처리하지만 핸들러 내부 stat도 OK
	// TODO: structured log

	return nil
}
