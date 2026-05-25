# SeumStandard v3.1 — 개발자 셋업

(철학·비전은 [`README.md`](./README.md) 참조. 이 문서는 빌드·테스트·통합 작업용.)

## 디렉토리

```
D:\FSoZ\
├── README.md                                 (철학 문서, 손대지 않음)
├── okneo31 안재현 스탠더드와 스마트컨츄랙.md   (원본 디자인)
│
├── jjjajh_STD_SC.sol                         Ethereum/EVM 컨트랙트 (Solidity 0.8.20, v3.1)
├── jjjajh_STD_Cal.html                       시뮬레이터 (v4.1, 5탭, kWR 라벨, 봉사자본 로직)
├── zion-integration.md                       양 컨트랙트 + zion1.top 브리지 명세
│
├── jjjajh_STD_CW\                            Zion CosmWasm Rust 컨트랙트
│   ├── Cargo.toml
│   ├── src\{lib,contract,msg,state,error,tests}.rs
│   └── README.md
│
├── test\
│   └── SeumStandard.test.js                  Hardhat 테스트 스캐폴드
├── hardhat.config.js
├── package.json
└── DEV_SETUP.md                              (이 문서)
```

## 빌드 & 테스트

### Solidity (EVM 측)
```bash
pnpm install                # 의존성: hardhat, ethers, chai
pnpm compile                # → artifacts/SeumStandard.json
pnpm test                   # test/SeumStandard.test.js 실행 (스캐폴드 — TODO 항목 구현 필요)
```

### CosmWasm (Zion 측)
```bash
cd jjjajh_STD_CW
cargo build --target wasm32-unknown-unknown --release
# 또는 reproducible build:
docker run --rm -v "$(pwd)":/code cosmwasm/optimizer:0.16.0
# → artifacts/seum_standard_cw.wasm
```

### 시뮬레이터
브라우저에서 `jjjajh_STD_Cal.html` 더블클릭. 외부 의존: Tailwind CDN + Chart.js CDN.

## 핵심 결정 (Spec Lock v7)

- **3축 명예**: C 자본 + L 노동 + V 검증, 가중치 1:5:5
- **단위**: kWR (Zion utrg). ETH 사용 안 함.
- **시민권 게이트**: Zion PoP 필수 (Path A 9-vouch / Path B mock-KYC)
- **감쇠율**: rate(n) = 1% × 1.01^n, 462일차 100% cap, 자연 세대교체
- **감쇠 면제**: 안식일 (가입일 기준 7일 주기) OR 그날 ≥2h 활동
- **봉사 (v3.1)**: 매주 ≥2h 무료노동 시 — (a) multBP × 1.01342 (52주=2×) + (b) 자본 += max(100 kWR, C × 1%)
- **VP**: (√C + 5√L + 5√V) × √(multBP / BP)
- **Ragequit**: EVM은 이벤트만 (zion1.top이 Zion에서 unlock). CW는 즉시 bank send.

## 양 컨트랙트 차이

| 기능 | Ethereum (Solidity) | Zion (CosmWasm Rust) |
|---|---|---|
| 자본 입금 | `bridgeMintCapital` + zion1.top 서명 | `ContributeCapital` + funds (utrg) 직접 |
| Ragequit | event → off-chain unlock | `BankMsg::Send` 즉시 |
| 신탁 검증 | ECDSA + EIP-191 | `info.sender == attester` |
| 비트맵 슬롯 | uint256 = 256일/슬롯 | u128 = 128일/슬롯 |

## 미해결 / Future 작업

| 항목 | 우선순위 | 비고 |
|---|---|---|
| Solidity 테스트 TODO 채우기 (감쇠, 봉사, 거버넌스, 합성) | High | test/SeumStandard.test.js |
| CW 단위 테스트 (cw-multi-test) | High | jjjajh_STD_CW/src/tests.rs |
| Zion 측 x/seum 모듈 PR | High | D:\ZION\ (구현 Zion 팀) |
| zion1.top 데몬 (Go) | High | watcher + signer + bridge |
| 멀티시그 attester (Phase 2) | Mid | v3.1 단일 키 → v4 멀티시그 |
| 시뮬 + CW 차이 보정 (256↔128 비트맵) | Low | 시뮬은 256bit 가정. CW는 128bit. 결과 동일하나 슬롯 키 다름 |
| ZION_WHITEPAPER.md 정독 후 통합점 추가 | Mid | D:\ZION\ZION_WHITEPAPER.md (278KB) |

## 참고

- 통합 명세: [`zion-integration.md`](./zion-integration.md)
- Zion 리포: `D:\ZION\` (github.com/okneo31/Zion, private)
- Spec Lock v7 결정 32개: 컨트랙트 헤더 주석 + memory MEMORY.md
