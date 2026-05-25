// attest_pop는 Zion x/seum의 EvmMappingRegistered 이벤트 → EVM SeumStandard.attestPoP 호출.
//
// 흐름:
//
//	1. EventEvmMappingRegistered{evm_addr} 수신
//	2. random 32바이트 nonce 생성 (consumed 체크는 EVM 측에서)
//	3. expiry = now + signature_ttl
//	4. digest = keccak256(abi.encode("ATTEST_POP", contract, worker, expiry, nonce))
//	5. EIP-191 personal_sign
//	6. EVM tx 송출 (attestPoP)
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

const EventPoPVerifiedForSeum = "zion.seum.v1.EventEvmMappingRegistered"

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
	metrics := MetricsOrNoop(h.hc.Metrics)

	evmAddrStr, ok := ev.Attributes["evm_addr"]
	if !ok {
		metrics.IncEventProcessed("attest_pop", "missing_attr")
		return fmt.Errorf("missing evm_addr attribute")
	}
	worker := common.HexToAddress(evmAddrStr)
	if (worker == common.Address{}) {
		metrics.IncEventProcessed("attest_pop", "zero_addr")
		return fmt.Errorf("evm_addr is zero")
	}

	var nonce [32]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		metrics.IncEventProcessed("attest_pop", "rand_err")
		return fmt.Errorf("rand nonce: %w", err)
	}

	expiry := uint64(time.Now().Unix() + h.hc.SignatureTTLSeconds)

	signStart := time.Now()
	digest, err := signer.PackAttestPoPDigest(h.hc.EVMClient.Address(), worker, expiry, nonce)
	if err != nil {
		metrics.IncEventProcessed("attest_pop", "pack_err")
		return fmt.Errorf("pack digest: %w", err)
	}
	v, r, s, err := h.hc.Signer.SignDigest(digest)
	if err != nil {
		metrics.IncEventProcessed("attest_pop", "sign_err")
		return fmt.Errorf("sign: %w", err)
	}
	metrics.ObserveSigningDuration("attest_pop", time.Since(signStart).Seconds())

	tx, err := h.hc.EVMClient.BroadcastAttestPoP(ctx, worker, expiry, nonce, v, r, s)
	if err != nil {
		metrics.IncEventProcessed("attest_pop", "broadcast_err")
		return fmt.Errorf("broadcast attestPoP: %w", err)
	}
	_ = tx

	metrics.IncEventProcessed("attest_pop", "ok")
	return nil
}
