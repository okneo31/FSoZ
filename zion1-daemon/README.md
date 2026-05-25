# zion1.top 데몬

SeumStandard ↔ Zion 브리지 서비스. Cosmos SDK (Zion) 체인 이벤트를 감시하고 EIP-191 서명으로 SeumStandard EVM 컨트랙트 트랜잭션을 송출합니다.

**상태**: v0.1-scaffold (2026-05-24). 컴파일 가능한 구조 — 핵심 비즈니스 로직 (ABI 인코딩, CometBFT 이벤트 디코딩) 은 `TODO:` 마커로 표시.

## 디렉토리

```
zion1-daemon/
├── go.mod
├── README.md
├── config.example.yaml
├── cmd/zion1-daemon/main.go        진입점
└── internal/
    ├── config/                     YAML 설정 로딩
    ├── signer/                     EIP-191 personal_sign (attester 키)
    ├── evmclient/                  SeumStandard 컨트랙트 호출 + 이벤트 감시
    ├── zionclient/                 CometBFT WS subscribe + 디코딩 (+ Mock)
    ├── handlers/                   이벤트 → action 매핑
    │   ├── handler.go              인터페이스 + Dispatcher
    │   └── attest_pop.go           ★ 작동 가능한 첫 핸들러
    ├── store/                      consumed 추적 (메모리 + JSON 파일)
    └── pipeline/                   메인 루프 + 재시도
```

## 빌드 & 실행

```bash
# 의존성
go mod tidy

# 빌드
go build -o bin/zion1-daemon ./cmd/zion1-daemon

# 설정 작성
cp config.example.yaml config.yaml
# 편집: attester.private_key_hex, contract_address, RPC URLs

# 실행
./bin/zion1-daemon -config config.yaml

# 또는 mock 모드 (Zion 미가동 환경)
# config.yaml에서 mock.enabled: true
```

## 작동 흐름

```
[Zion CometBFT]                                                    [EVM]
      │                                                              │
      │  EventEvmMappingRegistered{zion_addr, evm_addr}              │
      ├──────────────────────WS subscribe──────────► [zionclient]    │
      │                                                  │           │
      │                                                  ▼           │
      │                                              [pipeline]      │
      │                                                  │           │
      │                                      ┌───────────┼───────┐   │
      │                                      ▼           ▼       ▼   │
      │                                 [dispatcher → handler:           │
      │                                  attest_pop.go]                  │
      │                                                  │              │
      │                                          (digest + sign)        │
      │                                                  │              │
      │                                                  ▼              │
      │                                            [evmclient]          │
      │                                                  │              │
      │                                                  ▼              │
      │                            SeumStandard.attestPoP(evm_addr,...)│
      │                            ◄────────────EVM tx broadcast───────┤
      │                                                                 │
```

## 핵심 미해결 (TODO)

| 영역 | 위치 | 작업 |
|---|---|---|
| **ABI 인코딩** | `signer/signer.go` | `go-ethereum/accounts/abi`로 `abi.Arguments.Pack(...)` 사용. 현재 placeholder는 `keccak256(concat)`로 단순화됨 — Solidity의 `abi.encode`와 *맞지 않음* |
| **EVM tx 호출** | `evmclient/client.go` | `bind.NewKeyedTransactorWithChainID` + ABI bindings (`abigen`) — 컨트랙트 ABI에서 자동 생성하거나 수동 호출 |
| **CometBFT 디코딩** | `zionclient/client.go` | `types.EventDataTx`에서 `abci.Event[]` 추출 → attribute 맵 변환 |
| **이벤트 구독 query** | `cmd/main.go` | Zion 실제 event type 확정 후 (x/seum proto의 이벤트 이름) |
| **EVM RagequitRequest 감시** | `evmclient/client.go` + 새 핸들러 | `SubscribeFilterLogs`로 EVM 이벤트 → `MsgUnlockCapitalForSeum` 송출 (역방향 흐름) |
| **다른 핸들러** | `handlers/` | `bridge_mint.go`, `attest_honor.go`, `attest_day.go` — x/seum 명세 확정 후 |
| **재시도 큐 영구화** | `store/` | 현재 in-memory만. 재시작 시 처리 중 이벤트 손실 가능. SQLite/BoltDB 권장 |
| **Vault 키 로딩** | `signer/` + `config/` | 현재 평문 hex. HashiCorp Vault / AWS KMS 통합 |
| **메트릭** | (없음) | Prometheus exposition (event/sec, retry count, signing latency) |
| **단위 테스트** | (없음) | 핸들러 mock 기반, signer 정확성 검증 |

## 보안 노트

- **attester 개인키**: Phase 1 평문 hex. **운영 환경 절대 불가**. Phase 2에서 Vault/HSM 통합 필수.
- **EVM gas 페이**: 데몬이 EVM tx의 sender. gas wallet도 attester key로 사용 시 그 잔액 관리 필요. 별도 sender key 권장.
- **이벤트 신뢰**: WebSocket 이벤트만 보지 말고 chain query로 재확인. (현재 TODO.)
- **단일 실패점**: 데몬 다운 시 새 mapping/lock이 EVM 측에 반영 안 됨. Phase 2 = 멀티 인스턴스 + leader election.

## 참고

- 통합 명세: `D:\FSoZ\zion-integration.md`
- Zion x/seum 모듈 명세: `D:\FSoZ\zion-seum-module-spec.md`
- SeumStandard 컨트랙트: `D:\FSoZ\jjjajh_STD_SC.sol` (v3.1)
- Zion 컨벤션: `D:\ZION\zion-dev\CONVENTIONS.md`
