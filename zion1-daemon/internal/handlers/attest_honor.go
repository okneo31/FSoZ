// attest_honor는 Zion x/seum.EventHonorAttested → EVM attestHonor 호출.
//
//	EventHonorAttested{evm_addr, axis, honor_delta, job_id}
//
// axis: 1=LABOR, 2=VERIFICATION (0=CAPITAL은 컨트랙트가 reject — bridgeMintCapital로 가야).
// job_id가 replay nonce.
package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/okneo31/zion1-daemon/internal/signer"
	"github.com/okneo31/zion1-daemon/internal/zionclient"
)

const EventHonorAttested = "zion.seum.v1.EventHonorAttested"

type AttestHonorHandler struct {
	hc HandlerContext
}

func NewAttestHonorHandler(hc HandlerContext) *AttestHonorHandler {
	return &AttestHonorHandler{hc: hc}
}

func (h *AttestHonorHandler) EventTypes() []string {
	return []string{EventHonorAttested}
}

func (h *AttestHonorHandler) Handle(ctx context.Context, ev zionclient.TypedEvent) error {
	metrics := MetricsOrNoop(h.hc.Metrics)

	worker, err := requireAddrAttr(ev, "evm_addr")
	if err != nil {
		metrics.IncEventProcessed("attest_honor", "bad_addr")
		return err
	}
	axisU64, err := requireUint64Attr(ev, "axis")
	if err != nil {
		metrics.IncEventProcessed("attest_honor", "bad_axis")
		return err
	}
	if axisU64 > 2 {
		metrics.IncEventProcessed("attest_honor", "axis_out_of_range")
		return fmt.Errorf("axis must be 0..2, got %d", axisU64)
	}
	if axisU64 == 0 {
		metrics.IncEventProcessed("attest_honor", "capital_axis_blocked")
		return fmt.Errorf("CAPITAL axis (0) cannot be attested — use bridge_mint")
	}
	axis := uint8(axisU64)

	honorDelta, err := requireUintAttr(ev, "honor_delta")
	if err != nil {
		metrics.IncEventProcessed("attest_honor", "bad_delta")
		return err
	}

	jobID, err := requireBytes32Attr(ev, "job_id")
	if err != nil {
		metrics.IncEventProcessed("attest_honor", "bad_job_id")
		return err
	}

	expiry := uint64(time.Now().Unix() + h.hc.SignatureTTLSeconds)
	signStart := time.Now()
	digest, err := signer.PackAttestHonorDigest(h.hc.EVMClient.Address(), worker, axis, honorDelta, jobID, expiry)
	if err != nil {
		metrics.IncEventProcessed("attest_honor", "pack_err")
		return fmt.Errorf("pack digest: %w", err)
	}
	v, r, s, err := h.hc.Signer.SignDigest(digest)
	if err != nil {
		metrics.IncEventProcessed("attest_honor", "sign_err")
		return fmt.Errorf("sign: %w", err)
	}
	metrics.ObserveSigningDuration("attest_honor", time.Since(signStart).Seconds())

	_, err = h.hc.EVMClient.BroadcastAttestHonor(ctx, worker, axis, honorDelta, jobID, expiry, v, r, s)
	if err != nil {
		metrics.IncEventProcessed("attest_honor", "broadcast_err")
		return fmt.Errorf("broadcast attestHonor: %w", err)
	}
	metrics.IncEventProcessed("attest_honor", "ok")
	return nil
}
