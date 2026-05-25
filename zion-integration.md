# SeumStandard ↔ Zion 통합 명세

**버전**: v1.0 (2026-05-24, Spec Lock v7 기반)
**대상 컨트랙트**: SeumStandard v3.1 (Ethereum/EVM Solidity) + SeumStandard CosmWasm (Zion)
**브리지 주체**: zion1.top — Zion 체인 watcher / 단일 attester 서비스

---

## 0. 한 줄 요약

> **시민은 Zion에서 PoP를 통과하고, Zion에서 일감을 하고, Zion에서 kWR을 lock한다. 그 모든 행위가 zion1.top 의 서명을 거쳐 SeumStandard 컨트랙트(EVM 또는 Zion CW)에 명예로 적립된다.**

Zion = 사실의 출처(source of truth). SeumStandard = 명예(honor)의 누적·감쇠·합성 레이어.

---

## 1. 아키텍처 (양 컨트랙트 공존)

```
┌──────────────────────────── Zion Chain (Cosmos SDK) ──────────────────────────┐
│                                                                                │
│  x/pop      — Path A 9-vouch / Path B mock-KYC                                │
│  x/job      — 일감 lifecycle + escrow                                          │
│  x/review   — 3-panel review + 9-panel dispute                                 │
│  x/mirror   — 평판 + 다양성                                                     │
│  x/bankext  — 5-field wallet (available, locked_in_escrow, …)                 │
│                                                                                │
│         ▲                                            ▲                         │
│         │ EventPoPVerified                            │ EventJobCompleted      │
│         │ EventLockedForSeum                          │ EventDailyActivity     │
│         │ EventVolunteerCompleted                                              │
└─────────┼────────────────────────────────────────────┼─────────────────────────┘
          │                                            │
          │                                            │
          ▼                                            ▼
   ┌───────────────────────────────────────────────────────┐
   │  zion1.top  (단일 attester 서비스, Go/Rust 데몬)        │
   │   - WebSocket subscribe to Zion events                │
   │   - 검증 + 페이로드 정규화                                │
   │   - EIP-191 personal_sign (단일 키)                    │
   │   - EVM tx broadcast / CosmWasm tx broadcast          │
   └───────────────────────────────────────────────────────┘
          │                                            │
          │ EVM tx (Eth signature)                     │ Cosmos tx (Zion address)
          ▼                                            ▼
   ┌──────────────────────────┐              ┌────────────────────────────────┐
   │  SeumStandard v3.1       │              │  SeumStandard CosmWasm         │
   │  (Solidity, EVM)         │              │  (Rust, Zion contracts/)       │
   │                          │              │                                │
   │  attestPoP()             │              │  ExecuteMsg::AttestPoP {…}     │
   │  bridgeMintCapital()     │              │  ExecuteMsg::ContributeCapital │
   │  attestHonor()           │              │     {} (with funds = kWR)      │
   │  attestDay()             │              │  ExecuteMsg::AttestHonor {…}   │
   │  ragequit() → event      │              │  ExecuteMsg::AttestDay {…}     │
   │                          │              │  ExecuteMsg::Ragequit {}       │
   └──────────────────────────┘              │     (즉시 bank send)            │
                                              └────────────────────────────────┘
```

두 컨트랙트는 **독립 deployment**. 사용자는 본인 선호 체인에서 시민이 됨.
양쪽 모두 같은 3축·감쇠·안식일·봉사 규약 사용. 자본 입금 경로만 다름.

---

## 2. 핵심 데이터 흐름 (4가지)

### 2.1 PoP 등록 (Zion → EVM)

```
1. 사용자가 Zion에서 Path A (9-vouch) 또는 Path B (mock-KYC) 통과
   → x/pop이 EventPoPVerified(zion_addr) 발생

2. zion1.top watcher: 이벤트 감지
   - zion_addr → 사용자가 신고한 evm_addr 매핑 조회 (off-chain DB)
   - EIP-191 다이제스트 계산: keccak256(abi.encode(
         "ATTEST_POP", contract_addr, evm_addr, expiry, nonce))
   - personal_sign (단일 키)

3. zion1.top: EVM 트랜잭션 broadcast
   - SeumStandard.attestPoP(evm_addr, expiry, nonce, v, r, s)
   → attestedPoP[evm_addr] = true. PoPAttested 이벤트.

4. 사용자: join() 호출 가능
   - require(attestedPoP[msg.sender]) 통과
```

**Zion CW 측에선** 시민이 같은 Zion 체인에 있으므로 PoP 직접 조회 가능 — attestPoP 불필요.
대신 `Execute::Join {}`이 내부적으로 `x/pop.QueryPoPStatus(sender)`로 직접 확인.

### 2.2 자본 명예 발급 (kWR lock-mint)

```
EVM 경로:
  1. 사용자: zionnode tx seum lock-capital --amount 1000kWR --evm-addr 0xABC
     → x/seum (또는 x/bankext 확장)이 1000 kWR을 locked_in_escrow 상태로 이동
     → EventLockedForSeum(zion_addr, evm_addr, kWRAmount, lockId) 발생

  2. zion1.top watcher: 감지 → 서명
     digest = keccak256(abi.encode(
         "BRIDGE_MINT_CAPITAL", contract_addr, evm_addr, kWRAmount, lockId, expiry))

  3. zion1.top → EVM:
     SeumStandard.bridgeMintCapital(evm_addr, kWRAmount, lockId, expiry, v, r, s)
     → baseHonor[CAPITAL] += kWRAmount
     → lockedKWR += kWRAmount
     → consumed[lockId] = true

Zion CW 경로:
  1. 사용자: zionnode tx wasm execute SEUM_CW '{"contribute_capital":{}}' \
              --amount 1000utrg
     → CW 컨트랙트가 funds (utrg)를 escrow로 받음
     → baseHonor[CAPITAL] += amount, lockedKWR += amount
     → (kWR은 컨트랙트 주소 잔고에 누적)
```

→ 양 경로 모두 결과 동일: 자본 명예 += kWRAmount, lockedKWR += kWRAmount.

### 2.3 노동/검증 명예 발급 (Job 완료)

```
1. Zion x/job: 일감 완료 + x/review가 panel 검증 → reward 분배
   → 워커는 kWR 보상 (Zion 측)
   → EventJobAttestedForSeum(zion_addr, evm_addr, axis, honorDelta, jobId) 발생
     - axis: LABOR or VERIFICATION
     - honorDelta: x/mirror 평판 증분 (1:1 또는 가중 변환)

2. zion1.top watcher: 감지 → 서명
   digest = keccak256(abi.encode(
       "ATTEST_HONOR", contract_addr, evm_addr, axis, honorDelta, jobId, expiry))

3. zion1.top → EVM (또는 Zion CW):
   SeumStandard.attestHonor(evm_addr, axis, honorDelta, jobId, expiry, sig)
   → baseHonor[axis] += honorDelta (감쇠 적용 후)
```

**cancellation 처리** (사용자 비즈니스 룰):
- "마지막 보상 수령 시 거부" 흐름은 Zion x/job 책임
- 각종 수수료 제외 후 노동자보상만 일감등록자에게 반환
- 이 경우 EventJobAttestedForSeum 발생 안 함 → SeumStandard 명예 변동 없음

### 2.4 일별 활동 게이트 (자정 정산)

```
1. zion1.top watcher: 매일 UTC 자정 후 각 시민에 대해 집계
   - 그날의 노동+검증 시간 합계 ≥ 7200초 (2h)? → activeGateMet
   - 그날의 봉사 시간 합계 ≥ 7200초? → volunteerGateMet
   (봉사 = 일감 완료했으나 노동료 미수령 — Zion x/job의 isVolunteer 플래그)

2. 서명:
   digest = keccak256(abi.encode(
       "ATTEST_DAY", contract_addr, evm_addr, day, activeGateMet,
       volunteerGateMet, expiry, nonce))

3. EVM (또는 Zion CW):
   SeumStandard.attestDay(evm_addr, day, activeGateMet, volunteerGateMet, ...)
   → activeDayBits / volunteerDayBits 비트 set
```

비트맵 word packing — 256일 / uint256 슬롯.

### 2.5 Ragequit (탈퇴 + kWR 회수)

```
EVM 경로:
  1. 사용자: SeumStandard.ragequit()
     - amount = lockedKWR
     - 모든 상태 삭제 (citizens, hasBadge)
     - EventRagequitRequest(evm_addr, amount) 발생

  2. zion1.top watcher: 감지
     - evm_addr → zion_addr 매핑 조회
     - zionnode tx 송출:
       zionnode tx seum unlock-capital --evm-addr 0xABC --amount {amount}
     → Zion x/seum이 locked_in_escrow에서 zion_addr로 amount kWR 반환

Zion CW 경로:
  1. 사용자: zionnode tx wasm execute SEUM_CW '{"ragequit":{}}'
     - 컨트랙트가 lockedKWR 만큼 bank send (즉시)
     - 모든 상태 삭제
```

**주의**: 봉사로 얻은 자본 보너스 (lockedKWR에 안 들어간 honor 만)는 ragequit 시 소멸.
→ 이는 의도된 설계: 봉사 보너스는 "내 자본 가치"가 아니라 "내가 기여한 명예의 측정치".

---

## 3. 서명 다이제스트 형식 (EIP-191 personal_sign)

| 함수 | digest payload | 1회용 토큰 |
|---|---|---|
| `attestPoP` | `("ATTEST_POP", this, worker, expiry, nonce)` | nonce |
| `bridgeMintCapital` | `("BRIDGE_MINT_CAPITAL", this, worker, kWRAmount, lockId, expiry)` | lockId |
| `attestHonor` | `("ATTEST_HONOR", this, worker, axis, honorDelta, jobId, expiry)` | jobId |
| `attestDay` | `("ATTEST_DAY", this, worker, day, activeGateMet, volunteerGateMet, expiry, nonce)` | nonce |

각 다이제스트는 `keccak256(abi.encode(...))` → `keccak256("\x19Ethereum Signed Message:\n32" + digest)` → `ecrecover(...) == attester` 로 검증.

모든 1회용 토큰은 통합 `consumed[bytes32]` 매핑에 저장 (digest prefix가 type 구분).

**Expiry 권장값**: 24시간. 너무 짧으면 동기화 실패, 너무 길면 서명 탈취 시 영향 확대.

---

## 4. 봉사 자본 보너스 (v3.1 NEW)

매 자격 주마다:
- (a) `multBP *= 101342 / 100000` (≈ ×1.01342)
- (b) `baseHonor[CAPITAL] += max(VOLUNTEER_CAPITAL_MIN, capital × 1%)`
  - `VOLUNTEER_CAPITAL_MIN = 100` (kWR)
  - 임계점: 자본 10,000 kWR (이하 → +100 정액, 이상 → +1% 비율)

→ 양 컨트랙트 동일하게 적용. 보너스로 늘어난 자본은 **lockedKWR에 안 들어감** (Zion 측에 실제 kWR이 없음).
→ Ragequit 시 봉사로 늘어난 자본은 unlock 안 됨 — 명예 측정치로만 작용.

**5년 매주 봉사 + 초기 C=10,000 kWR 시**:
- multBP: 32× → VP factor √32 ≈ 5.66×
- 자본 (보너스만): 10000 × 1.01^260 ≈ 132,000 kWR → √13.2 ≈ 3.6×
- VP 종합: ~20× 증가 (봉사 효과 자체로)

---

## 5. Zion 측 필요 작업 (구현은 Zion 팀)

SeumStandard 통합을 위해 Zion 체인에 신규 모듈 `x/seum` 또는 기존 모듈 확장 필요.

### 5.1 신규 메시지 (proto/zion/seum/v1/)

| Msg | 필드 | 효과 |
|---|---|---|
| `MsgLockCapitalForSeum` | sender, evm_addr, amount, lock_id | available → locked_in_escrow. EventLockedForSeum 발생 |
| `MsgUnlockCapitalForSeum` | attester, evm_addr, amount | locked_in_escrow → sender (zion_addr 매핑 필요). attester 서명 검증 |
| `MsgRegisterEvmMapping` | sender, evm_addr, signature | zion_addr ↔ evm_addr 양방향 매핑 등록 |

### 5.2 신규 이벤트

| Event | 필드 |
|---|---|
| `EventPoPVerifiedForSeum` | zion_addr, evm_addr, path (A/B) |
| `EventLockedForSeum` | zion_addr, evm_addr, amount, lock_id |
| `EventJobAttestedForSeum` | zion_addr, evm_addr, axis, honor_delta, job_id |
| `EventDailyActivityForSeum` | evm_addr, day, active_gate_met, volunteer_gate_met |

### 5.3 zion1.top 데몬 의무

- WebSocket subscribe to Zion CometBFT events
- evm_addr 매핑 조회 (x/seum.QueryEvmMapping)
- 이벤트 검증 (예: EventLockedForSeum 의 lock_id가 정말 lock 상태인지)
- EIP-191 서명 (단일 키, Vault 보호)
- EVM tx broadcast (gas 페이) — gas 풀 필요
- 동시에 Zion CW 컨트랙트 호출 (`MsgExecuteContract`)
- 실패 시 재시도 + 알람

---

## 6. 보안 고려사항

| 위험 | 완화책 |
|---|---|
| zion1.top 키 탈취 | v3.1 단일 키 (v4 멀티시그 계획). Hot wallet은 Vault/HSM 권장 |
| Replay attack | 모든 함수에 `consumed[id]` 1회용 토큰 |
| Front-running (양 컨트랙트) | EOA → EOA: 영향 없음. Bot이 attestPoP 가로채도 동일 결과 |
| evm_addr-zion_addr 매핑 오류 | `MsgRegisterEvmMapping`은 양방향 서명 (zion_addr가 evm_addr의 서명을 제출) |
| 동일 사용자 양 컨트랙트 활용 | 각 컨트랙트가 독립 시민 상태. 의도된 설계 — 사용자가 원하면 양쪽 모두 시민 |
| Ragequit 후 봉사 보너스 손실 | 의도된 설계 — Spec Lock v6.1 결정. 명세에 명시 |
| zion1.top 다운 | Zion 측 이벤트는 누적. 데몬 재기동 시 catch-up. EVM 측 honor는 attest 늦어진 만큼 감쇠 영향 — 사용자에게 SLA 통지 필요 |

---

## 7. 배포·운영 체크리스트

### Phase 1 (single-attester)
- [ ] zion1.top 데몬 ed25519/secp256k1 키 생성 → Vault에 보관
- [ ] 키의 EVM 주소를 SeumStandard constructor `attester` 파라미터에 주입
- [ ] Zion x/seum 모듈 구현 (또는 x/bankext 확장)
- [ ] Zion CW 컨트랙트 배포 (jjjajh_STD_CW 별도 작업)
- [ ] EVM SeumStandard v3.1 배포 + verify on Etherscan
- [ ] zion1.top 데몬 실행 (testnet 먼저)
- [ ] 양 컨트랙트 E2E 테스트: PoP → join → lock → bridge → attest → ragequit

### Phase 2 (멀티시그 + 분산)
- [ ] attester를 multisig (3-of-5)로 전환 → rotateAttester 호출
- [ ] zion1.top을 N개 독립 데몬으로 분산
- [ ] 합의된 서명만 EVM 수락

---

## 8. 참고

- 컨트랙트 코드: `D:\FSoZ\jjjajh_STD_SC.sol` (Ethereum v3.1)
- CosmWasm 코드: `D:\FSoZ\jjjajh_STD_CW\` (예정)
- 시뮬레이터: `D:\FSoZ\jjjajh_STD_Cal.html` (v4.1, kWR 라벨)
- Zion 리포지토리: `D:\ZION\` (clone of github.com/okneo31/Zion)
- Spec Lock v7 (32+ 결정): 이 문서 + 컨트랙트 헤더 주석 참조

---

**작성**: jjjajh @ okneo31 (안재현)
**다음 갱신**: Zion x/seum 모듈 PR 작성 시 + Zion CW 컨트랙트 완성 시
