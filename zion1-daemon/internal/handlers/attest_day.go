// attest_day는 Zion x/seum.EventDayAttested → EVM attestDay 호출.
//
//	EventDayAttested{evm_addr, day, active_gate, volunteer_gate, nonce}
//
// day는 컨트랙트의 day index (block.timestamp/86400). active/volunteer는 OR.
// nonce는 replay 방지 — 한 day에 multi-attest 가능하므로 (다른 nonce면 다른 attest).
package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/okneo31/zion1-daemon/internal/signer"
	"github.com/okneo31/zion1-daemon/internal/zionclient"
)

const EventDayAttested = "zion.seum.v1.EventDayAttested"

type AttestDayHandler struct {
	hc HandlerContext
}

func NewAttestDayHandler(hc HandlerContext) *AttestDayHandler {
	return &AttestDayHandler{hc: hc}
}

func (h *AttestDayHandler) EventTypes() []string {
	return []string{EventDayAttested}
}

func (h *AttestDayHandler) Handle(ctx context.Context, ev zionclient.TypedEvent) error {
	metrics := MetricsOrNoop(h.hc.Metrics)

	worker, err := requireAddrAttr(ev, "evm_addr")
	if err != nil {
		metrics.IncEventProcessed("attest_day", "bad_addr")
		return err
	}
	day, err := requireUint64Attr(ev, "day")
	if err != nil {
		metrics.IncEventProcessed("attest_day", "bad_day")
		return err
	}
	activeGate, err := requireBoolAttr(ev, "active_gate")
	if err != nil {
		metrics.IncEventProcessed("attest_day", "bad_active")
		return err
	}
	volunteerGate, err := requireBoolAttr(ev, "volunteer_gate")
	if err != nil {
		metrics.IncEventProcessed("attest_day", "bad_volunteer")
		return err
	}
	if !activeGate && !volunteerGate {
		// 양쪽 다 false면 호출 의미 없음 — silent drop
		metrics.IncEventProcessed("attest_day", "no_op")
		return nil
	}
	nonce, err := requireBytes32Attr(ev, "nonce")
	if err != nil {
		metrics.IncEventProcessed("attest_day", "bad_nonce")
		return err
	}

	expiry := uint64(time.Now().Unix() + h.hc.SignatureTTLSeconds)
	signStart := time.Now()
	digest, err := signer.PackAttestDayDigest(h.hc.EVMClient.Address(), worker, day, activeGate, volunteerGate, expiry, nonce)
	if err != nil {
		metrics.IncEventProcessed("attest_day", "pack_err")
		return fmt.Errorf("pack digest: %w", err)
	}
	v, r, s, err := h.hc.Signer.SignDigest(digest)
	if err != nil {
		metrics.IncEventProcessed("attest_day", "sign_err")
		return fmt.Errorf("sign: %w", err)
	}
	metrics.ObserveSigningDuration("attest_day", time.Since(signStart).Seconds())

	_, err = h.hc.EVMClient.BroadcastAttestDay(ctx, worker, day, activeGate, volunteerGate, expiry, nonce, v, r, s)
	if err != nil {
		metrics.IncEventProcessed("attest_day", "broadcast_err")
		return fmt.Errorf("broadcast attestDay: %w", err)
	}
	metrics.IncEventProcessed("attest_day", "ok")
	return nil
}

// 컴파일 사용 확인 — fmt 정의됨.
var _ = fmt.Sprintf
