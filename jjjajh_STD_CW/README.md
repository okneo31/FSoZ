# SeumStandard CosmWasm v3.1

Zion 체인용 CosmWasm 컨트랙트. EVM Solidity v3.1과 **비트 동일 로직**.

## 빌드

```bash
cargo wasm
# 또는 최적화:
docker run --rm -v "$(pwd)":/code \
  --mount type=volume,source="$(basename "$(pwd)")_cache",target=/target \
  --mount type=volume,source=registry_cache,target=/usr/local/cargo/registry \
  cosmwasm/optimizer:0.16.0
```

## 배포 (Zion)

```bash
# 1. 코드 업로드
zionnode tx wasm store artifacts/seum_standard_cw.wasm \
  --from operator --chain-id zion-local-1 --gas auto

# 2. 인스턴스화
zionnode tx wasm instantiate $CODE_ID \
  '{"attester":"zion1...","governance":"zion1...","kwr_denom":"utrg"}' \
  --label "SeumStandard v3.1" --from operator --no-admin

# 3. PoP 등록 (attester만)
zionnode tx wasm execute $CONTRACT \
  '{"attest_pop":{"worker":"zion1..."}}' --from attester

# 4. 시민 가입
zionnode tx wasm execute $CONTRACT '{"join":{}}' --from user

# 5. 자본 입금 (kWR과 함께)
zionnode tx wasm execute $CONTRACT '{"contribute_capital":{}}' \
  --amount 1000utrg --from user
```

## 구조

```
src/
├── lib.rs          진입점
├── contract.rs     instantiate / execute / query 핸들러 + 핵심 로직
├── msg.rs          ExecuteMsg / QueryMsg / Response
├── state.rs        상수 + Citizen 구조체 + Storage
├── error.rs        ContractError
└── tests.rs        단위 테스트 (TODO)
```

## v3.1 정합 핵심

- 3축 명예 (Capital, Labor, Verification) 가중치 1:5:5
- rate(n) = 1% × 1.01^n 가속 감쇠 (462일차 100% cap)
- 안식일 + 활동 게이트 → 그날 감쇠 면제
- 매주 봉사: multBP × 1.01342 + **자본 += max(100, C × 1%)** (v3.1 NEW)
- VP = (√C + 5√L + 5√V) × √(multBP/BP)
- 비트맵 word packing — u128/슬롯, 128일/슬롯 (Solidity uint256 = 256일/슬롯, Rust u128 = 128일/슬롯)

## EVM 컨트랙트와 차이

| 부분 | EVM (Solidity) | Zion (CosmWasm Rust) |
|---|---|---|
| 자본 입금 | `bridgeMintCapital` + zion1.top 서명 검증 | `ContributeCapital {}` + funds (utrg) 직접 |
| Ragequit | 이벤트 → off-chain unlock | `BankMsg::Send` 즉시 송금 |
| 신탁 검증 | `ecrecover(EIP-191 digest)` | `info.sender == attester` 직접 |
| 비트맵 슬롯 | uint256 = 256일 | u128 = 128일 |
| PoP | `attestedPoP[addr]` (zion1.top이 set) | 동일 — Zion 체인이지만 PoP는 별개 attestation 유지 |

## 참고

- 통합 명세: `D:\FSoZ\zion-integration.md`
- EVM 컨트랙트: `D:\FSoZ\jjjajh_STD_SC.sol`
- 시뮬레이터: `D:\FSoZ\jjjajh_STD_Cal.html`
