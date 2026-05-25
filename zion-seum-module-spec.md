# Zion `x/seum` 모듈 명세 (PROPOSAL)

**버전**: v0.1-draft (2026-05-24)
**대상**: Zion 체인 (Cosmos SDK 0.50, CometBFT 0.38) — `chain/x/seum/` 신규 모듈
**작성**: jjjajh @ okneo31 (SeumStandard 프로젝트 측)
**상태**: 제안서 (Zion 팀 리뷰 대기)

---

## 0. 한 줄 요약

> **Awakened가 SeumStandard EVM 컨트랙트(또는 Zion CosmWasm 컨트랙트)에서 자본 명예를 누적할 수 있도록 kWR을 escrow하고, 이벤트로 zion1.top watcher에 알림. EVM ragequit 시 attester 서명으로 unlock.**

기존 `x/bankext`의 5-field wallet (`available / locked_in_escrow / pending_debt / staked / vesting`)을 그대로 활용 — `x/seum`은 escrow의 *seum-bridge 서브계정*을 관리할 뿐.

---

## 1. 동기

SeumStandard 컨트랙트 (`D:\FSoZ\jjjajh_STD_SC.sol`, v3.1) 는:
- **자산 단위**: kWR (Zion utrg)
- **자본 입금**: Zion에서 kWR을 lock → zion1.top이 서명 → EVM `bridgeMintCapital`
- **Ragequit**: EVM 이벤트 → zion1.top 감지 → Zion에서 unlock

Zion 측에 이 lock/unlock 흐름을 처리하는 모듈이 필요. 기존 모듈로는 부족:
- `x/bankext`: lock 상태는 있지만 *seum-bridge용 lock*을 다른 escrow와 구분 안 됨
- `x/job`: 일감 escrow 전용 — bridge용 lock과 의미 다름

→ **`x/seum` 신규 모듈**로 명확히 분리.

---

## 2. 모듈 구조 (Zion 컨벤션 준수)

```
chain/x/seum/
├── README.md
├── abci.go                  BeginBlock/EndBlock (현 v0.1 = 비어 있음)
├── module.go                AppModuleBasic + AppModule 표준
├── keeper/
│   ├── keeper.go            Keeper 구조체 + bankext/pop 의존성 주입
│   ├── msg_server.go        MsgServer 구현 (3개 메시지)
│   ├── query_server.go      QueryServer 구현
│   ├── events.go            이벤트 발생 helper
│   ├── verifier.go          attester 서명 검증 (ed25519 또는 secp256k1)
│   ├── mapping.go           evm_addr ↔ zion_addr 매핑 CRUD
│   └── escrow.go            seum-bridge escrow 잠금/해제 (x/bankext 위임)
├── types/
│   ├── codec.go
│   ├── errors.go
│   ├── events.go            이벤트 type 상수
│   ├── expected_keepers.go  BankExtKeeper, PopKeeper 인터페이스
│   ├── genesis.go
│   ├── keys.go              StoreKey, prefixes
│   ├── msgs.go              Msg ValidateBasic
│   ├── params.go            모듈 파라미터 (attester pubkey 등)
│   └── types.pb.go          (proto 생성)
└── proto -> ../../../proto/zion/seum/v1/
```

Proto 위치 (Zion 컨벤션):
```
proto/zion/seum/v1/
├── events.proto
├── genesis.proto
├── params.proto
├── query.proto
├── state.proto
└── tx.proto
```

---

## 3. State

### 3.1 신규 KV store (prefix `seum/`)

| Key | Value | 설명 |
|---|---|---|
| `seum/lock/{lock_id}` | `BridgeLock` proto | lock 상태 |
| `seum/by_zion_addr/{zion_addr}/{lock_id}` | `[]byte{}` | secondary index |
| `seum/mapping/zion_to_evm/{zion_addr}` | `[]byte(evm_addr_hex)` | 매핑 (양방향) |
| `seum/mapping/evm_to_zion/{evm_addr_hex}` | `[]byte(zion_addr)` | 매핑 (양방향) |
| `seum/next_lock_id` | `uint64` (gogo little-endian) | auto-increment counter |
| `seum/params` | `Params` proto | 모듈 파라미터 |

### 3.2 BridgeLock 메시지

```proto
message BridgeLock {
  uint64 lock_id = 1;
  string zion_addr = 2;          // bech32 (zion1...)
  string evm_addr = 3;           // hex with 0x prefix
  string amount = 4;             // utrg, sdk.Int string
  int64 created_height = 5;
  int64 created_time = 6;        // UTC seconds
  LockStatus status = 7;
  int64 settled_height = 8;      // unlock 또는 forfeit 시점
}

enum LockStatus {
  LOCK_STATUS_UNSPECIFIED = 0;
  LOCK_STATUS_LOCKED = 1;        // EVM mint 가능 상태
  LOCK_STATUS_RELEASED = 2;      // EVM ragequit 후 unlock 완료
  LOCK_STATUS_FORFEITED = 3;     // (v2+) 슬래시 등 — Phase 1은 사용 안 함
}
```

### 3.3 5-field wallet 통합 (x/bankext)

`x/seum`은 kWR을 직접 보관하지 않음 — 모두 `x/bankext`에 위임.

- `MsgLockCapitalForSeum` 처리 시: `bankext.MoveToLocked(zion_addr, amount, "seum")` 호출. tag="seum" 으로 다른 escrow와 구분.
- `MsgUnlockCapitalForSeum` 처리 시: `bankext.MoveFromLocked(zion_addr, amount, "seum")` 호출.

→ `x/bankext`에 `LockReason` 또는 `tag` 필드 추가 필요 (또는 기존 메커니즘 활용). 자세한 인터페이스는 §5.

---

## 4. Messages (Tx)

### 4.1 `MsgRegisterEvmMapping`

자기 zion_addr ↔ evm_addr 등록. 양방향 서명 필요 (zion_addr 측은 Cosmos 서명, evm_addr 측은 EIP-191 서명 첨부).

```proto
message MsgRegisterEvmMapping {
  option (cosmos.msg.v1.signer) = "zion_addr";

  string zion_addr = 1;
  string evm_addr = 2;                   // 0x... hex
  bytes evm_signature = 3;               // EIP-191 personal_sign of (zion_addr)
}

message MsgRegisterEvmMappingResponse {}
```

**검증**:
- `zion_addr`이 Awakened인지 확인 (`x/pop.IsAwakened`)
- `evm_signature`가 evm_addr의 개인키로 서명된 `zion_addr` 메시지인지 검증 (`ecrecover`)
- 이미 매핑된 zion_addr/evm_addr이면 거부 (1:1 강제)

**이벤트**: `EventEvmMappingRegistered(zion_addr, evm_addr)`

→ EVM 측 `attestedPoP`은 이 이벤트를 보고 zion1.top이 `attestPoP(evm_addr, ...)` 트랜잭션 송출.

### 4.2 `MsgLockCapitalForSeum`

Awakened가 자기 kWR을 seum-bridge escrow로 lock. 결과 lock_id가 EVM의 jobId 역할.

```proto
message MsgLockCapitalForSeum {
  option (cosmos.msg.v1.signer) = "zion_addr";

  string zion_addr = 1;
  cosmos.base.v1beta1.Coin amount = 2 [(gogoproto.nullable) = false];   // utrg
  string evm_addr = 3;                   // 대상 EVM 주소 (mapping과 일치해야 함)
}

message MsgLockCapitalForSeumResponse {
  uint64 lock_id = 1;
}
```

**검증**:
- `zion_addr` Awakened
- `mapping[zion_addr] == evm_addr`
- `amount.denom == "utrg"`, `amount.amount > 0`
- `available >= amount` (bankext 위임)

**처리**:
1. `bankext.MoveToLocked(zion_addr, amount, "seum")`
2. `lock_id = nextLockId++`
3. `BridgeLock{lock_id, zion_addr, evm_addr, amount, ..., LOCKED}` 저장
4. `EventLockedForSeum(lock_id, zion_addr, evm_addr, amount)` 발생

→ zion1.top watcher가 이벤트 감지 → EIP-191 서명 → EVM `bridgeMintCapital(evm_addr, amount, lock_id, ...)` 송출.

### 4.3 `MsgUnlockCapitalForSeum`

`attester` (zion1.top)만 호출 가능. EVM ragequit 이벤트 감지 후 송출.

```proto
message MsgUnlockCapitalForSeum {
  option (cosmos.msg.v1.signer) = "attester";

  string attester = 1;                   // params.attester_addr 와 일치
  string evm_addr = 2;
  cosmos.base.v1beta1.Coin amount = 3 [(gogoproto.nullable) = false];
  bytes ragequit_tx_hash = 4;            // EVM 이벤트의 tx hash (리플레이 차단)
}

message MsgUnlockCapitalForSeumResponse {}
```

**검증**:
- `attester == params.attester_addr`
- `mapping[evm_addr] != ""` (매핑 존재)
- `consumed[ragequit_tx_hash] == false` (리플레이 차단)
- `amount.denom == "utrg"`
- 해당 zion_addr의 `bankext.GetLocked(zion_addr, "seum") >= amount`

**처리**:
1. `consumed[ragequit_tx_hash] = true`
2. `zion_addr = mapping[evm_addr]`
3. `bankext.MoveFromLocked(zion_addr, amount, "seum")` → available로 복귀
4. 모든 해당 zion_addr의 LOCKED 상태 BridgeLock을 RELEASED로 갱신 (또는 FIFO 가산)
5. `EventUnlockedForSeum(evm_addr, zion_addr, amount, ragequit_tx_hash)` 발생

---

## 5. `x/bankext` 측 변경 (Dependency)

### 5.1 신규 인터페이스

`bankext.Keeper`에 *tagged escrow* 지원 추가:

```go
// chain/x/bankext/keeper/escrow.go (신규 또는 확장)

// MoveToLocked는 available에서 locked_in_escrow로 amount를 이동. tag로 escrow 종류 구분.
func (k Keeper) MoveToLocked(ctx context.Context, addr sdk.AccAddress, amount sdk.Coin, tag string) error

// MoveFromLocked는 tag별 locked에서 available로 amount를 복귀.
func (k Keeper) MoveFromLocked(ctx context.Context, addr sdk.AccAddress, amount sdk.Coin, tag string) error

// GetLocked는 tag별 locked 잔액 조회.
func (k Keeper) GetLocked(ctx context.Context, addr sdk.AccAddress, tag string) sdk.Coin
```

**기존 코드 영향**:
- 현재 `locked_in_escrow`는 단일 통합 필드. tag 도입 시 storage 마이그레이션 필요 (기존 lock = `tag="job"` 또는 `"legacy"` 등으로 라벨링).
- 또는 *별도 storage*로 분리: `seum_locked_in_escrow` 필드 신설 (5-field → 6-field 확장). 이 방식은 마이그레이션 가벼우나 wallet 표시 UI 갱신 필요.

→ **권장**: tag 도입 (확장성 ↑). x/job, x/poms 등도 향후 활용 가능.

### 5.2 expected_keepers.go (x/seum 측)

```go
// chain/x/seum/types/expected_keepers.go
type BankExtKeeper interface {
    MoveToLocked(ctx context.Context, addr sdk.AccAddress, amount sdk.Coin, tag string) error
    MoveFromLocked(ctx context.Context, addr sdk.AccAddress, amount sdk.Coin, tag string) error
    GetLocked(ctx context.Context, addr sdk.AccAddress, tag string) sdk.Coin
}

type PopKeeper interface {
    IsAwakened(ctx context.Context, addr sdk.AccAddress) bool
}
```

---

## 6. Events

```proto
// proto/zion/seum/v1/events.proto

message EventEvmMappingRegistered {
  string zion_addr = 1;
  string evm_addr = 2;
  int64 height = 3;
}

message EventLockedForSeum {
  uint64 lock_id = 1;
  string zion_addr = 2;
  string evm_addr = 3;
  string amount = 4;        // utrg sdk.Int string
  int64 height = 5;
}

message EventUnlockedForSeum {
  string evm_addr = 1;
  string zion_addr = 2;
  string amount = 3;
  bytes ragequit_tx_hash = 4;
  int64 height = 5;
}
```

`zion1.top` 데몬은 CometBFT WebSocket에서 이 3개 이벤트를 subscribe 후 EVM 트랜잭션 송출.

---

## 7. Query API

```proto
service Query {
  rpc Params(QueryParamsRequest) returns (QueryParamsResponse);
  rpc Lock(QueryLockRequest) returns (QueryLockResponse);
  rpc LocksByZion(QueryLocksByZionRequest) returns (QueryLocksByZionResponse);
  rpc Mapping(QueryMappingRequest) returns (QueryMappingResponse);
}
```

zion1.top이 catch-up 시 사용.

---

## 8. Params

```proto
message Params {
  string attester_addr = 1;         // zion1.top의 zion_addr (또는 별도 키)
  string attester_pubkey_hex = 2;   // EIP-191 검증용 EVM 측 pubkey (선택)
  string min_lock_amount = 3;       // utrg, sdk.Int. 예: "1000000" (1 kWR)
  bool   enabled = 4;               // 글로벌 kill switch (governance)
}
```

거버넌스로 변경 가능. attester 키 회전은 standard `MsgUpdateParams` (cosmos-sdk v0.50 패턴).

---

## 9. Invariants

```go
// 등록할 invariant — chain/app/app.go의 RegisterInvariants에서

func TotalLockedInvariant(k Keeper, bkk BankExtKeeper) sdk.Invariant {
    // sum(BridgeLock.amount where LOCKED) == bankext.GetLocked(*, "seum") 총합
    // → x/seum의 lock 합계가 bankext의 tag="seum" 잔액과 일치
}

func MappingBijectionInvariant(k Keeper) sdk.Invariant {
    // mapping[zion_to_evm]과 mapping[evm_to_zion]은 양방향 일대일
}

func MonotonicLockIdInvariant(k Keeper) sdk.Invariant {
    // next_lock_id는 모든 저장된 BridgeLock의 lock_id보다 큼
}
```

→ Zion의 42개 chain-wide invariants에 합류.

---

## 10. Genesis

```go
type GenesisState struct {
    Params      Params
    Locks       []BridgeLock
    Mappings    []EvmMapping
    NextLockId  uint64
    Consumed    [][]byte         // ragequit_tx_hashes 1회용
}
```

Phase 1 (테스트넷) 초기값:
- `enabled = false` (배포 후 거버넌스로 활성화)
- `min_lock_amount = "1000000"` (1 kWR)
- `attester_addr`: 배포자가 별도 설정

---

## 11. 시큐리티

| 위험 | 완화 |
|---|---|
| attester 키 탈취 | Phase 1 단일 키, Phase 2 멀티시그 (`x/seum` MsgUnlockCapitalForSeum signer를 multisig로) |
| 동일 ragequit 이벤트 두 번 처리 | `consumed[ragequit_tx_hash]` 1회용 |
| mapping 가로채기 (front-run) | `MsgRegisterEvmMapping`이 EIP-191 서명 검증 — EVM 측 키 보유자만 매핑 가능 |
| Lock 잡고 unlock 안 함 | `MsgCancelLock` 등 사용자 측 회수 메커니즘 추가 검토 (v2+) — Phase 1은 attester 신뢰 |
| bankext tag 충돌 | `x/seum`은 항상 `tag="seum"` 사용. 다른 모듈과 충돌 없음 |

---

## 12. 테스트 전략

```
chain/x/seum/keeper/keeper_test.go        — 기본 keeper CRUD
chain/x/seum/keeper/msg_server_test.go    — 3개 메시지 happy path + 부정 경로
chain/x/seum/keeper/mapping_test.go       — EIP-191 서명 검증 + 가로채기 시도
chain/x/seum/keeper/escrow_test.go        — bankext 통합 — lock/unlock 흐름
tests/e2e/seum_bridge_test.go             — 풀 lock → mint → ragequit → unlock 시나리오
```

E2E는 EVM Hardhat과 Zion localnet을 함께 띄우고 zion1.top 데몬 mock으로 실행.

---

## 13. Phase 1 v.s. Phase 2

**Phase 1 (이 명세)**:
- 단일 attester (zion1.top)
- 위 3개 Msg + 3개 Event
- bankext에 tag 도입
- Invariant 3개

**Phase 2 (별도 명세)**:
- 멀티시그 attester
- Lock 만료/자동 회수 (사용자가 attester 응답 없을 때)
- IBC 통합 (다른 코스모스 체인에서 SeumStandard 사용)
- Slashing (악의적 attester 처벌)

---

## 14. 작업 분량 추정

| 단계 | 추정 |
|---|---|
| proto 정의 + buf generate | 1일 |
| bankext tag 도입 + 마이그레이션 | 2~3일 |
| x/seum keeper + types | 3~4일 |
| msg_server + query_server | 2~3일 |
| 단위 테스트 | 3~4일 |
| E2E 테스트 + Hardhat 통합 mock | 3~5일 |
| Invariant + 거버넌스 통합 | 1~2일 |
| **합계** | **15~22일** (~3~4주, 1인 풀타임 기준) |

---

## 15. 다음 작업

1. **이 명세 Zion 팀 리뷰** — 컨벤션, 의존성, 우선순위
2. **Zion `zion-dev/tasks/TASK-60-seum-module.md`** 로 정식 task 등재
3. **PR 분할**:
   - PR-1: proto 정의
   - PR-2: bankext tag 도입
   - PR-3: x/seum 모듈 (keeper + types)
   - PR-4: msg_server + 테스트
   - PR-5: E2E + Hardhat mock
4. **병행**: zion1.top 데몬 (Go) 구현 (별도 작업, x/seum 명세 확정 후)

---

## 16. 참고

- SeumStandard EVM 컨트랙트: `D:\FSoZ\jjjajh_STD_SC.sol` (v3.1)
- SeumStandard CW 컨트랙트: `D:\FSoZ\jjjajh_STD_CW\` (참고용 — Zion 측은 x/seum + bankext로 동등 기능)
- 통합 명세: `D:\FSoZ\zion-integration.md` (브리지 흐름 전체)
- Zion 컨벤션: `D:\ZION\zion-dev\CONVENTIONS.md`
- Zion 아키텍처: `D:\ZION\zion-dev\ARCHITECTURE.md` (또는 `docs/ARCHITECTURE.md`)

---

**작성자 노트**: 이 명세는 SeumStandard 측 가정에 기반. Zion 팀이 다른 통합 방식을 선호할 수 있음 — 예:
- (a) 새 모듈 없이 `x/job` 확장으로 처리 (job category에 "seum_lock" 추가)
- (b) IBC를 통한 통신만 사용
- (c) x/bankext 직접 확장

각각 trade-off 다름. 명세 v0.2에서 결정.
