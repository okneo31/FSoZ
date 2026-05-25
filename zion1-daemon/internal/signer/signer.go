// Package signer는 EIP-191 personal_sign 다이제스트 생성/서명.
// SeumStandard v3.1 컨트랙트의 모든 attester 검증 함수 (attestPoP, bridgeMintCapital,
// attestHonor, attestDay)에 사용되는 서명 형식과 일치.
//
// 다이제스트 형식 (Solidity 컨트랙트와 비트 동일):
//
//	digest  = keccak256(abi.encode(...))                              ← 컨트랙트 내부
//	ethHash = keccak256("\x19Ethereum Signed Message:\n32" || digest) ← EIP-191
//	sig     = ecdsa.Sign(ethHash, attesterPrivKey)                    ← personal_sign
//
// abi.encode는 *고정* 32바이트 패딩이라 정확한 type 순서가 중요. Solidity contract와
// 단 1바이트라도 다르면 ecrecover ≠ attester 가 되어 EVM 측에서 reject.
package signer

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// DigestSigner는 32바이트 digest를 받아 (v,r,s) 서명 + attester 주소 반환.
// Phase 1.5에서 Vault/HSM 백엔드로 swap 가능하도록 interface로 분리.
type DigestSigner interface {
	Address() common.Address
	SignDigest(digest []byte) (v uint8, r, s [32]byte, err error)
}

// LocalSigner는 메모리에 평문 개인키 보유 (Phase 1).
// 운영 환경에서는 VaultSigner 사용 권장.
type LocalSigner struct {
	privKey *ecdsa.PrivateKey
	addr    common.Address
}

// NewLocal은 hex 인코딩된 개인키에서 LocalSigner 생성. 64 hex chars (no 0x prefix).
func NewLocal(privKeyHex string) (*LocalSigner, error) {
	privKeyHex = strings.TrimPrefix(privKeyHex, "0x")
	if len(privKeyHex) != 64 {
		return nil, fmt.Errorf("private key must be 64 hex chars, got %d", len(privKeyHex))
	}
	keyBytes, err := hex.DecodeString(privKeyHex)
	if err != nil {
		return nil, fmt.Errorf("decode hex: %w", err)
	}
	privKey, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse ECDSA key: %w", err)
	}
	pubKey, ok := privKey.Public().(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key not ECDSA")
	}
	addr := crypto.PubkeyToAddress(*pubKey)
	return &LocalSigner{privKey: privKey, addr: addr}, nil
}

// PrivateKey returns the underlying ECDSA key — needed by evmclient for tx 서명.
// VaultSigner는 이 메소드 제공 안 함 (key 유출 방지) — 그 경우 별도 tx-signing 키 사용.
func (s *LocalSigner) PrivateKey() *ecdsa.PrivateKey { return s.privKey }

// Address는 attester의 EVM 주소 (서명 검증용).
func (s *LocalSigner) Address() common.Address { return s.addr }

// SignDigest는 raw 32바이트 digest를 EIP-191 personal_sign 형식으로 서명.
// 반환: v, r, s — Solidity ecrecover와 직접 호환.
func (s *LocalSigner) SignDigest(digest []byte) (v uint8, r, sValue [32]byte, err error) {
	if len(digest) != 32 {
		err = fmt.Errorf("digest must be 32 bytes, got %d", len(digest))
		return
	}

	prefix := []byte("\x19Ethereum Signed Message:\n32")
	ethHash := crypto.Keccak256(append(prefix, digest...))

	sig, err := crypto.Sign(ethHash, s.privKey)
	if err != nil {
		err = fmt.Errorf("sign: %w", err)
		return
	}
	if len(sig) != 65 {
		err = fmt.Errorf("signature length 65 expected, got %d", len(sig))
		return
	}

	copy(r[:], sig[:32])
	copy(sValue[:], sig[32:64])
	v = sig[64] + 27 // ethereum convention: v in {27, 28}
	return
}

// ─── ABI 타입 lazy init (전역 — 매 호출마다 NewType 비용 회피) ───

var (
	stringT  abi.Type
	addressT abi.Type
	uint8T   abi.Type
	uint64T  abi.Type
	uint256T abi.Type
	boolT    abi.Type
	bytes32T abi.Type
)

func init() {
	var err error
	if stringT, err = abi.NewType("string", "", nil); err != nil {
		panic(err)
	}
	if addressT, err = abi.NewType("address", "", nil); err != nil {
		panic(err)
	}
	if uint8T, err = abi.NewType("uint8", "", nil); err != nil {
		panic(err)
	}
	if uint64T, err = abi.NewType("uint64", "", nil); err != nil {
		panic(err)
	}
	if uint256T, err = abi.NewType("uint256", "", nil); err != nil {
		panic(err)
	}
	if boolT, err = abi.NewType("bool", "", nil); err != nil {
		panic(err)
	}
	if bytes32T, err = abi.NewType("bytes32", "", nil); err != nil {
		panic(err)
	}
}

// ─── Digest pack helpers — 각 컨트랙트 함수와 1:1 매칭 ───

// PackAttestPoPDigest는 SeumStandard.attestPoP의 다이제스트.
//
//	abi.encode("ATTEST_POP", address(this), worker, expiry, nonce)
//	types:    (string,      address,        address, uint64, bytes32)
func PackAttestPoPDigest(contract, worker common.Address, expiry uint64, nonce [32]byte) ([]byte, error) {
	args := abi.Arguments{
		{Type: stringT}, {Type: addressT}, {Type: addressT},
		{Type: uint64T}, {Type: bytes32T},
	}
	packed, err := args.Pack("ATTEST_POP", contract, worker, expiry, nonce)
	if err != nil {
		return nil, fmt.Errorf("abi pack ATTEST_POP: %w", err)
	}
	return crypto.Keccak256(packed), nil
}

// PackBridgeMintCapitalDigest는 SeumStandard.bridgeMintCapital의 다이제스트.
//
//	abi.encode("BRIDGE_MINT_CAPITAL", address(this), worker, kWRAmount, lockId, expiry)
//	types:    (string,                address,        address, uint256,   bytes32, uint64)
func PackBridgeMintCapitalDigest(contract, worker common.Address, kWRAmount *big.Int, lockID [32]byte, expiry uint64) ([]byte, error) {
	args := abi.Arguments{
		{Type: stringT}, {Type: addressT}, {Type: addressT},
		{Type: uint256T}, {Type: bytes32T}, {Type: uint64T},
	}
	packed, err := args.Pack("BRIDGE_MINT_CAPITAL", contract, worker, kWRAmount, lockID, expiry)
	if err != nil {
		return nil, fmt.Errorf("abi pack BRIDGE_MINT_CAPITAL: %w", err)
	}
	return crypto.Keccak256(packed), nil
}

// PackAttestHonorDigest는 SeumStandard.attestHonor의 다이제스트.
//
//	abi.encode("ATTEST_HONOR", address(this), worker, axis, honorDelta, jobId, expiry)
//	types:    (string,        address,        address, uint8, uint256, bytes32, uint64)
//
// axis: 0=CAPITAL, 1=LABOR, 2=VERIFICATION (CAPITAL은 컨트랙트가 reject)
func PackAttestHonorDigest(contract, worker common.Address, axis uint8, honorDelta *big.Int, jobID [32]byte, expiry uint64) ([]byte, error) {
	args := abi.Arguments{
		{Type: stringT}, {Type: addressT}, {Type: addressT},
		{Type: uint8T}, {Type: uint256T}, {Type: bytes32T}, {Type: uint64T},
	}
	packed, err := args.Pack("ATTEST_HONOR", contract, worker, axis, honorDelta, jobID, expiry)
	if err != nil {
		return nil, fmt.Errorf("abi pack ATTEST_HONOR: %w", err)
	}
	return crypto.Keccak256(packed), nil
}

// PackAttestDayDigest는 SeumStandard.attestDay의 다이제스트.
//
//	abi.encode("ATTEST_DAY", address(this), worker, day, activeGate, volunteerGate, expiry, nonce)
//	types:    (string,      address,        address, uint64, bool,   bool,          uint64, bytes32)
func PackAttestDayDigest(contract, worker common.Address, day uint64, activeGate, volunteerGate bool, expiry uint64, nonce [32]byte) ([]byte, error) {
	args := abi.Arguments{
		{Type: stringT}, {Type: addressT}, {Type: addressT},
		{Type: uint64T}, {Type: boolT}, {Type: boolT}, {Type: uint64T}, {Type: bytes32T},
	}
	packed, err := args.Pack("ATTEST_DAY", contract, worker, day, activeGate, volunteerGate, expiry, nonce)
	if err != nil {
		return nil, fmt.Errorf("abi pack ATTEST_DAY: %w", err)
	}
	return crypto.Keccak256(packed), nil
}
