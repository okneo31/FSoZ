// Package signer는 EIP-191 personal_sign 다이제스트 생성/서명.
// SeumStandard v3.1 컨트랙트의 모든 attester 검증 함수 (attestPoP, bridgeMintCapital,
// attestHonor, attestDay)에 사용되는 서명 형식과 일치.
//
// 다이제스트:
//   digest = keccak256(abi.encode(...))                              ← 컨트랙트에서 사용
//   ethHash = keccak256("\x19Ethereum Signed Message:\n32" + digest)  ← EIP-191
//   sig = ecdsa.Sign(ethHash, attesterPrivKey)                       ← personal_sign
package signer

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// Signer는 attester 키를 보관하며 EIP-191 다이제스트를 서명.
type Signer struct {
	privKey *ecdsa.PrivateKey
	addr    common.Address
}

// New는 hex 인코딩된 개인키에서 Signer 생성. 64 hex chars (no 0x prefix).
func New(privKeyHex string) (*Signer, error) {
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
	return &Signer{privKey: privKey, addr: addr}, nil
}

// Address는 attester의 EVM 주소 (서명 검증용).
func (s *Signer) Address() common.Address {
	return s.addr
}

// SignDigest는 raw 32바이트 digest를 EIP-191 personal_sign 형식으로 서명.
// 반환: v, r, s — Solidity ecrecover와 직접 호환.
func (s *Signer) SignDigest(digest []byte) (v uint8, r, sValue [32]byte, err error) {
	if len(digest) != 32 {
		err = fmt.Errorf("digest must be 32 bytes, got %d", len(digest))
		return
	}

	// EIP-191: prepend "\x19Ethereum Signed Message:\n32" to the raw digest
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

// PackAttestPoPDigest는 SeumStandard.attestPoP의 다이제스트 계산.
//   payload: ("ATTEST_POP", contract, worker, expiry, nonce)
func PackAttestPoPDigest(contract, worker common.Address, expiry uint64, nonce [32]byte) []byte {
	// abi.encode 형식: 각 인자 32바이트로 패딩.
	// string은 dynamic — 별도 처리. abi.encode("ATTEST_POP", ...)는 [offset, encoded_string, ...]
	// 단순화를 위해 abi.encodePacked 대신 같은 의미의 raw concatenation 사용 시 컨트랙트와 차이 가능.
	// 정확한 호환을 위해 go-ethereum의 abi 패키지 사용 권장 — 이 함수는 그 추상화의 entry point.
	//
	// TODO: 실제 abi.encode 구현 — go-ethereum의 abi.Arguments.Pack(...) 사용.
	// 일단 placeholder.
	return packAbiEncodeAttestPoP("ATTEST_POP", contract, worker, expiry, nonce)
}

// PackBridgeMintCapitalDigest는 SeumStandard.bridgeMintCapital의 다이제스트.
//   payload: ("BRIDGE_MINT_CAPITAL", contract, worker, kWRAmount, lockId, expiry)
func PackBridgeMintCapitalDigest(contract, worker common.Address, kWRAmount *big.Int, lockId [32]byte, expiry uint64) []byte {
	return packAbiEncodeBridgeMint("BRIDGE_MINT_CAPITAL", contract, worker, kWRAmount, lockId, expiry)
}

// 실제 abi.encode 호환 구현은 go-ethereum의 abi 패키지로.
// 이 stub은 컴파일을 위한 placeholder — 실제 사용 시 abi.Arguments.Pack(...) 호출 필수.
func packAbiEncodeAttestPoP(label string, contract, worker common.Address, expiry uint64, nonce [32]byte) []byte {
	// TODO: 진짜 구현은 다음과 같이:
	//   stringType, _ := abi.NewType("string", "", nil)
	//   addressType, _ := abi.NewType("address", "", nil)
	//   uint64Type, _ := abi.NewType("uint64", "", nil)
	//   bytes32Type, _ := abi.NewType("bytes32", "", nil)
	//   args := abi.Arguments{
	//     {Type: stringType}, {Type: addressType}, {Type: addressType},
	//     {Type: uint64Type}, {Type: bytes32Type},
	//   }
	//   packed, _ := args.Pack(label, contract, worker, expiry, nonce)
	//   return crypto.Keccak256(packed)
	return crypto.Keccak256([]byte(label), contract.Bytes(), worker.Bytes())
}

func packAbiEncodeBridgeMint(label string, contract, worker common.Address, kWRAmount *big.Int, lockId [32]byte, expiry uint64) []byte {
	return crypto.Keccak256([]byte(label), contract.Bytes(), worker.Bytes(), kWRAmount.Bytes(), lockId[:])
}
