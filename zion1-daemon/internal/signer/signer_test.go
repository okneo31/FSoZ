package signer

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// 테스트 키 — 결정론 ECDSA (Hardhat #0 등 통상 정해진 dev key를 흉내).
// secret = "1" (그냥 1 ECDSA scalar).
const testPrivKey = "0000000000000000000000000000000000000000000000000000000000000001"

func TestNewLocal_DerivesAddress(t *testing.T) {
	s, err := NewLocal(testPrivKey)
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	if s.Address() == (common.Address{}) {
		t.Fatalf("address is zero")
	}
	// 0x prefix 허용
	s2, err := NewLocal("0x" + testPrivKey)
	if err != nil {
		t.Fatalf("NewLocal with 0x: %v", err)
	}
	if s.Address() != s2.Address() {
		t.Fatalf("0x prefix changed address: %s vs %s", s.Address(), s2.Address())
	}
}

func TestNewLocal_RejectsBadInput(t *testing.T) {
	cases := []struct{ name, key string }{
		{"too short", "abc"},
		{"too long", "aa" + testPrivKey},
		{"non-hex", "zz00000000000000000000000000000000000000000000000000000000000000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewLocal(tc.key); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestSignDigest_RoundtripsEcrecover(t *testing.T) {
	s, err := NewLocal(testPrivKey)
	if err != nil {
		t.Fatal(err)
	}
	// 임의 32바이트 digest
	digest := crypto.Keccak256([]byte("hello"))
	v, r, sBytes, err := s.SignDigest(digest)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}

	// EIP-191 ethHash 재구성
	prefix := []byte("\x19Ethereum Signed Message:\n32")
	ethHash := crypto.Keccak256(append(prefix, digest...))

	// 65바이트 signature 재구성 (v ∈ {27,28} → {0,1})
	sig := make([]byte, 65)
	copy(sig[0:32], r[:])
	copy(sig[32:64], sBytes[:])
	sig[64] = v - 27

	// ecrecover
	pubKey, err := crypto.SigToPub(ethHash, sig)
	if err != nil {
		t.Fatalf("SigToPub: %v", err)
	}
	recovered := crypto.PubkeyToAddress(*pubKey)
	if recovered != s.Address() {
		t.Fatalf("recovered %s != signer %s", recovered, s.Address())
	}
}

func TestSignDigest_RejectsWrongLength(t *testing.T) {
	s, err := NewLocal(testPrivKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.SignDigest([]byte("short")); err == nil {
		t.Fatal("expected error for short digest")
	}
}

// ─── ABI Pack 테스트 ───

func TestPackAttestPoPDigest_Deterministic(t *testing.T) {
	contract := common.HexToAddress("0x1111111111111111111111111111111111111111")
	worker := common.HexToAddress("0x2222222222222222222222222222222222222222")
	var nonce [32]byte
	copy(nonce[:], crypto.Keccak256([]byte("nonce1")))
	expiry := uint64(1_700_000_000)

	d1, err := PackAttestPoPDigest(contract, worker, expiry, nonce)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := PackAttestPoPDigest(contract, worker, expiry, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(d1, d2) {
		t.Fatal("digest not deterministic")
	}
	if len(d1) != 32 {
		t.Fatalf("digest len = %d, want 32", len(d1))
	}
}

func TestPackAttestPoPDigest_DifferentInputsDifferentDigests(t *testing.T) {
	contract := common.HexToAddress("0x1111111111111111111111111111111111111111")
	worker := common.HexToAddress("0x2222222222222222222222222222222222222222")
	var nonce1, nonce2 [32]byte
	copy(nonce1[:], crypto.Keccak256([]byte("nonce1")))
	copy(nonce2[:], crypto.Keccak256([]byte("nonce2")))
	expiry := uint64(1_700_000_000)

	d1, _ := PackAttestPoPDigest(contract, worker, expiry, nonce1)
	d2, _ := PackAttestPoPDigest(contract, worker, expiry, nonce2)
	if bytes.Equal(d1, d2) {
		t.Fatal("different nonces produced same digest")
	}
}

func TestPackAllDigests_DistinctLabels(t *testing.T) {
	// 각 함수가 자기 label만 쓰는지 — POP과 HONOR digest는 inputs 일부 겹쳐도 달라야.
	contract := common.HexToAddress("0x1111111111111111111111111111111111111111")
	worker := common.HexToAddress("0x2222222222222222222222222222222222222222")
	var nonce, jobID, lockID [32]byte
	copy(nonce[:], crypto.Keccak256([]byte("n")))
	copy(jobID[:], crypto.Keccak256([]byte("j")))
	copy(lockID[:], crypto.Keccak256([]byte("l")))
	expiry := uint64(1_700_000_000)

	pop, _ := PackAttestPoPDigest(contract, worker, expiry, nonce)
	honor, _ := PackAttestHonorDigest(contract, worker, 1 /*LABOR*/, big.NewInt(100), jobID, expiry)
	day, _ := PackAttestDayDigest(contract, worker, 18000, true, false, expiry, nonce)
	bridge, _ := PackBridgeMintCapitalDigest(contract, worker, big.NewInt(1000), lockID, expiry)

	all := [][]byte{pop, honor, day, bridge}
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if bytes.Equal(all[i], all[j]) {
				t.Fatalf("digests %d and %d collide", i, j)
			}
		}
	}
}

func TestPackAttestPoPDigest_KnownVector(t *testing.T) {
	// 이 테스트는 Solidity 컨트랙트 측에서 같은 입력으로 abi.encode + keccak256 한 결과와 *같아야* 한다.
	// 실측 비교 (Hardhat test fixture)는 별도 통합 테스트에서 — 여기선 *내적 일관성* 만:
	//   1. 결과가 정해진 32바이트
	//   2. NULL 입력에도 panic 없음
	contract := common.Address{}
	worker := common.Address{}
	var nonce [32]byte
	d, err := PackAttestPoPDigest(contract, worker, 0, nonce)
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 32 {
		t.Fatalf("len=%d", len(d))
	}
	// 결과는 비결정 환경에 의존하지 않아야 — Solidity 측에서 같은 zero 입력으로 abi.encode +
	// keccak256 한 결과를 재현 가능. 실제 hex는 컨트랙트 헤더 주석/테스트와 동일해야 — 이 값을
	// 향후 cross-check로 hardcode 가능.
	_ = hex.EncodeToString(d)
}

// ─── Cross-check: ethers.js와 비트 동일 ───
// 입력·기대 digest는 tools/print-digests.js 가 ethers.AbiCoder + ethers.keccak256으로 생성.
// 이 4개 vector가 통과해야 Go signer가 Solidity ecrecover와 호환된다고 보증.

const (
	fixContract    = "0x1111111111111111111111111111111111111111"
	fixWorker      = "0x2222222222222222222222222222222222222222"
	fixExpiry      = uint64(1_700_000_000)
	fixNoncePopHex = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fixLockIDHex   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fixJobIDHex    = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	fixNonceDayHex = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

	fixKWRAmount   = 12345
	fixAxisLabor   = 1
	fixHonorDelta  = 5000
	fixDay         = uint64(18000)

	// 기대 출력 (ethers.js, 2026-05-25 검증):
	expectedPoP    = "32a2040be71fbf664278d7190129661e36da7c624c84abb52f642cc04b252ddc"
	expectedBridge = "ca1f91f220dd44436b2896638e535b2321ff96615f8226437ae718b3f26eb67e"
	expectedHonor  = "57846eb692bf6949f27196a86a1762ad0e6619173c11361382cc48946b97590a"
	expectedDay    = "88fced36e3d9c90e18a7ada5560d060c35987303077aa672a236bc097f1be6f7"
)

func hexBytes32(t *testing.T, h string) [32]byte {
	t.Helper()
	b, err := hex.DecodeString(h)
	if err != nil {
		t.Fatalf("hex decode %s: %v", h, err)
	}
	if len(b) != 32 {
		t.Fatalf("hex len %d != 32", len(b))
	}
	var out [32]byte
	copy(out[:], b)
	return out
}

func assertDigest(t *testing.T, label string, got []byte, expectedHex string) {
	t.Helper()
	gotHex := hex.EncodeToString(got)
	if gotHex != expectedHex {
		t.Fatalf("%s digest mismatch\n  got:      %s\n  expected: %s\n  (입력 변경 시 tools/print-digests.js 재실행)",
			label, gotHex, expectedHex)
	}
}

func TestCrossCheck_AttestPoP_ethersJS(t *testing.T) {
	contract := common.HexToAddress(fixContract)
	worker := common.HexToAddress(fixWorker)
	nonce := hexBytes32(t, fixNoncePopHex)
	got, err := PackAttestPoPDigest(contract, worker, fixExpiry, nonce)
	if err != nil {
		t.Fatal(err)
	}
	assertDigest(t, "ATTEST_POP", got, expectedPoP)
}

func TestCrossCheck_BridgeMintCapital_ethersJS(t *testing.T) {
	contract := common.HexToAddress(fixContract)
	worker := common.HexToAddress(fixWorker)
	lockID := hexBytes32(t, fixLockIDHex)
	got, err := PackBridgeMintCapitalDigest(contract, worker, big.NewInt(fixKWRAmount), lockID, fixExpiry)
	if err != nil {
		t.Fatal(err)
	}
	assertDigest(t, "BRIDGE_MINT_CAPITAL", got, expectedBridge)
}

func TestCrossCheck_AttestHonor_ethersJS(t *testing.T) {
	contract := common.HexToAddress(fixContract)
	worker := common.HexToAddress(fixWorker)
	jobID := hexBytes32(t, fixJobIDHex)
	got, err := PackAttestHonorDigest(contract, worker, fixAxisLabor, big.NewInt(fixHonorDelta), jobID, fixExpiry)
	if err != nil {
		t.Fatal(err)
	}
	assertDigest(t, "ATTEST_HONOR", got, expectedHonor)
}

func TestCrossCheck_AttestDay_ethersJS(t *testing.T) {
	contract := common.HexToAddress(fixContract)
	worker := common.HexToAddress(fixWorker)
	nonce := hexBytes32(t, fixNonceDayHex)
	got, err := PackAttestDayDigest(contract, worker, fixDay, true, false, fixExpiry, nonce)
	if err != nil {
		t.Fatal(err)
	}
	assertDigest(t, "ATTEST_DAY", got, expectedDay)
}
