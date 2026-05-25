// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title  SeumStandard v3.1 (okneo31 안재현 스탠더드 v3.1)
 * @notice 3축 명예 + 행동 게이트 + 안식일 + 봉사 복리 — kWR 브리지 통합판.
 *         "명예는 단일 정의가 아니다. 자본·노동·검증, 셋이 다 필요하다."
 *
 * v3.1 패치 (jjjajh_STD_SC, 2026-05-24, Spec Lock v7 = kWR 브리지 + PoP + 봉사자본):
 *   ⓪ 자산 단위: **kWR (Zion utrg)** — ETH 사용 안 함. 자본 명예는 Zion에서 lock된 kWR을
 *      zion1.top 신탁이 서명·중계해서 EVM에 mint하는 패턴 (lock-mint-release).
 *   ① 시민권 게이트: **Zion PoP (Path A 9-vouch / Path B mock-KYC) 통과 주소만** join 가능.
 *      `attestPoP`로 zion1.top이 서명 → on-chain `attestedPoP[addr]=true`.
 *   ② 자본 입금: `bridgeMintCapital(worker, kWRAmount, lockId, sig)` — Zion 측 lock 이벤트를
 *      zion1.top이 서명 → EVM 자본 명예 += kWRAmount, lockedKWR += kWRAmount.
 *   ③ 노동/검증: `attestHonor` (그대로). 일별 게이트: `attestDay`.
 *   ④ 봉사: 매 자격 주마다 (a) multBP × 1.01342 + (b) **자본 += max(100, C/100)** (NEW).
 *      → 봉사가 곱셈 보너스 + 최소 안전망 자본 동시 제공.
 *   ⑤ Ragequit: 자산 송금 *없음*. `EventRagequitRequest(worker, kWRAmount)` 발생 →
 *      zion1.top watcher가 Zion에서 unlock. 시민 상태는 즉시 삭제.
 *   ⑥ 감쇠 rate(n) = 1% × 1.01^n, 안식일/활동 게이트, 462일 ~100% — v3 그대로.
 *   ⑦ MAX_DECAY_DAYS_PER_CALL = 90, 비트맵 word packing — v3 그대로.
 *   ⑧ EIP-191 personal_sign 다이제스트 + 통합 `consumed` 매핑 (jobId/nonce/lockId 1회용).
 */
contract SeumStandard {

    // ─── 정수 결정론 (스탠더드 ①번 축, 결정 #98) ───
    uint256 public constant BASIS_POINTS            = 10000;
    uint256 public constant BASE_DECAY_BP           = 100;
    uint256 public constant DECAY_GROWTH_NUM        = 101;
    uint256 public constant DECAY_GROWTH_DEN        = 100;
    uint256 public constant VOLUNTEER_BONUS_NUM     = 101342;
    uint256 public constant VOLUNTEER_BONUS_DEN     = 100000;
    uint256 public constant VOLUNTEER_CAPITAL_MIN   = 100;    // 봉사 시 최소 자본 보너스
    uint256 public constant VOLUNTEER_CAPITAL_PCT   = 100;    // 1% (BP form: 100 of 10000)
    uint256 public constant SECONDS_PER_DAY         = 86400;
    uint64  public constant MAX_DECAY_DAYS_PER_CALL = 90;
    uint8   public constant DEFAULT_SABBATH_OFFSET  = 6;
    uint256 public constant INITIAL_CAPITAL_HONOR   = 1;
    uint256 public constant LABOR_WEIGHT            = 5;
    uint256 public constant VERIFICATION_WEIGHT     = 5;

    // ─── 거버넌스 + 신탁 ───
    address public governance;
    address public attester;    // zion1.top — Zion 체인 watcher / 서명자

    modifier onlyGovernance() { require(msg.sender == governance, "Only governance"); _; }

    enum Axis  { CAPITAL, LABOR, VERIFICATION }
    enum Badge { GOVERNANCE, AUDIT, DISPUTE, NODE }

    struct Citizen {
        bool    isCitizen;
        uint64  joinDay;
        uint8   sabbathOffset;
        uint256 lockedKWR;                                    // Zion에서 lock된 kWR 누적 (ragequit 시 unlock 대상)
        uint256[3] baseHonor;
        uint64[3]  lastDecayDay;
        uint256[3] decayRateAtLastDay;
        uint64  volunteerWeeks;
        uint64  lastProcessedWeek;
        uint256 volunteerMultBP;
    }

    mapping(address => Citizen) public citizens;
    mapping(address => mapping(Badge => bool)) public hasBadge;
    mapping(address => bool) public attestedPoP;              // Zion PoP 통과 주소 (join 전제조건)

    // 비트맵: 256일/uint256 슬롯
    mapping(address => mapping(uint256 => uint256)) public activeDayBits;
    mapping(address => mapping(uint256 => uint256)) public volunteerDayBits;

    // 리플레이 방지 — 통합 namespace (다이제스트의 prefix가 type 구분)
    mapping(bytes32 => bool) public consumed;

    // ─── 이벤트 ───
    event CitizenBorn(address indexed citizen, uint64 joinDay);
    event PoPAttested(address indexed worker);
    event CapitalBridged(address indexed worker, uint256 kWRAmount, bytes32 lockId);
    event HonorAttested(address indexed worker, Axis indexed axis, uint256 honorDelta, bytes32 jobId);
    event DayAttested(address indexed worker, uint64 indexed day, bool active, bool volunteer);
    event SabbathDaySet(address indexed citizen, uint8 offset);
    event RagequitRequest(address indexed citizen, uint256 kWRAmount);   // watcher가 Zion에서 unlock
    event BadgeGranted(address indexed citizen, Badge badge);
    event BadgeRevoked(address indexed citizen, Badge badge);
    event AttesterRotated(address indexed newAttester);
    event VolunteerWeekCounted(address indexed citizen, uint64 weekIdx, uint64 totalWeeks);
    event VolunteerCapitalBonus(address indexed citizen, uint256 bonus);  // v3.1 NEW

    constructor(address _attester) {
        require(_attester != address(0), "Zero attester");
        governance = msg.sender;
        attester   = _attester;
    }

    // ─────────────────────────────────────────────────────────
    // PoP 게이트 — Zion에서 검증된 주소만 시민이 될 수 있음
    // ─────────────────────────────────────────────────────────

    /**
     * @notice Zion x/pop 모듈에서 검증된 주소를 EVM에 등록.
     * @dev    zion1.top watcher가 Zion PoP 이벤트(Path A 완료 / Path B mock-KYC 통과) 감지 후 서명.
     *         payload: ("ATTEST_POP", this, worker, expiry, nonce)
     */
    function attestPoP(
        address worker,
        uint64  expiry,
        bytes32 nonce,
        uint8 v, bytes32 r, bytes32 s
    ) external {
        require(block.timestamp <= expiry, "Expired");
        require(!consumed[nonce], "Replay");

        bytes32 digest = keccak256(abi.encode(
            "ATTEST_POP", address(this), worker, expiry, nonce
        ));
        bytes32 ethHash = keccak256(abi.encodePacked(
            "\x19Ethereum Signed Message:\n32", digest
        ));
        require(ecrecover(ethHash, v, r, s) == attester, "Bad sig");

        consumed[nonce] = true;
        attestedPoP[worker] = true;
        emit PoPAttested(worker);
    }

    // ─────────────────────────────────────────────────────────
    // 시민권 — 가입, 안식일 지정
    // ─────────────────────────────────────────────────────────

    /**
     * @notice 가입 (비결제). Zion PoP 통과 필수.
     * @dev    자본 명예는 INITIAL_CAPITAL_HONOR=1로 시작. 자본은 bridgeMintCapital로만 누적.
     */
    function join() external {
        require(attestedPoP[msg.sender], "Zion PoP required");
        require(!citizens[msg.sender].isCitizen, "Already a citizen");
        uint64 today = uint64(block.timestamp / SECONDS_PER_DAY);
        Citizen storage c = citizens[msg.sender];
        c.isCitizen       = true;
        c.joinDay         = today;
        c.sabbathOffset   = DEFAULT_SABBATH_OFFSET;
        c.baseHonor[uint8(Axis.CAPITAL)]                = INITIAL_CAPITAL_HONOR;
        c.lastDecayDay[uint8(Axis.CAPITAL)]             = today;
        c.lastDecayDay[uint8(Axis.LABOR)]               = today;
        c.lastDecayDay[uint8(Axis.VERIFICATION)]        = today;
        c.decayRateAtLastDay[uint8(Axis.CAPITAL)]       = BASE_DECAY_BP;
        c.decayRateAtLastDay[uint8(Axis.LABOR)]         = BASE_DECAY_BP;
        c.decayRateAtLastDay[uint8(Axis.VERIFICATION)]  = BASE_DECAY_BP;
        c.lastProcessedWeek = today / 7;
        c.volunteerMultBP   = BASIS_POINTS;
        emit CitizenBorn(msg.sender, today);
    }

    function setSabbathDay(uint8 offset) external {
        require(citizens[msg.sender].isCitizen, "Not a citizen");
        require(offset < 7, "Offset 0~6");
        citizens[msg.sender].sabbathOffset = offset;
        emit SabbathDaySet(msg.sender, offset);
    }

    // ─────────────────────────────────────────────────────────
    // 자본 명예 — kWR 브리지 (lock-mint)
    // ─────────────────────────────────────────────────────────

    /**
     * @notice 자본 명예 발급. Zion에서 lock된 kWR에 대해 zion1.top이 서명.
     * @dev    payload: ("BRIDGE_MINT_CAPITAL", this, worker, kWRAmount, lockId, expiry).
     *         lockId 1회용. lockedKWR이 증가 — ragequit 시 unlock 대상.
     *         시간 게이트 보호는 자동 없음 (M5).
     */
    function bridgeMintCapital(
        address worker,
        uint256 kWRAmount,
        bytes32 lockId,
        uint64  expiry,
        uint8 v, bytes32 r, bytes32 s
    ) external {
        require(attestedPoP[worker], "Zion PoP required");
        require(citizens[worker].isCitizen, "Worker not citizen");
        require(kWRAmount > 0, "Zero amount");
        require(block.timestamp <= expiry, "Expired");
        require(!consumed[lockId], "Replay");

        bytes32 digest = keccak256(abi.encode(
            "BRIDGE_MINT_CAPITAL", address(this), worker, kWRAmount, lockId, expiry
        ));
        bytes32 ethHash = keccak256(abi.encodePacked(
            "\x19Ethereum Signed Message:\n32", digest
        ));
        require(ecrecover(ethHash, v, r, s) == attester, "Bad sig");

        consumed[lockId] = true;
        _applyDecay(worker, Axis.CAPITAL);
        citizens[worker].baseHonor[uint8(Axis.CAPITAL)] += kWRAmount;
        citizens[worker].lockedKWR += kWRAmount;
        emit CapitalBridged(worker, kWRAmount, lockId);
    }

    // ─────────────────────────────────────────────────────────
    // zion1.top 신탁 — 노동/검증 명예 + 일별 게이트
    // ─────────────────────────────────────────────────────────

    function attestHonor(
        address worker,
        Axis    axis,
        uint256 honorDelta,
        bytes32 jobId,
        uint64  expiry,
        uint8 v, bytes32 r, bytes32 s
    ) external {
        require(axis != Axis.CAPITAL, "Capital via bridgeMintCapital");
        require(attestedPoP[worker], "Zion PoP required");
        require(citizens[worker].isCitizen, "Worker not citizen");
        require(block.timestamp <= expiry, "Expired");
        require(!consumed[jobId], "Replay");

        bytes32 digest = keccak256(abi.encode(
            "ATTEST_HONOR", address(this), worker, axis, honorDelta, jobId, expiry
        ));
        bytes32 ethHash = keccak256(abi.encodePacked(
            "\x19Ethereum Signed Message:\n32", digest
        ));
        require(ecrecover(ethHash, v, r, s) == attester, "Bad sig");

        consumed[jobId] = true;
        _applyDecay(worker, axis);
        citizens[worker].baseHonor[uint8(axis)] += honorDelta;
        emit HonorAttested(worker, axis, honorDelta, jobId);
    }

    function attestDay(
        address worker,
        uint64  day,
        bool    activeGateMet,
        bool    volunteerGateMet,
        uint64  expiry,
        bytes32 nonce,
        uint8 v, bytes32 r, bytes32 s
    ) external {
        require(attestedPoP[worker], "Zion PoP required");
        require(citizens[worker].isCitizen, "Worker not citizen");
        require(block.timestamp <= expiry, "Expired");
        require(!consumed[nonce], "Replay");
        require(day <= block.timestamp / SECONDS_PER_DAY, "Future day");
        require(day >= citizens[worker].joinDay, "Before join");

        bytes32 digest = keccak256(abi.encode(
            "ATTEST_DAY", address(this), worker, day, activeGateMet, volunteerGateMet, expiry, nonce
        ));
        bytes32 ethHash = keccak256(abi.encodePacked(
            "\x19Ethereum Signed Message:\n32", digest
        ));
        require(ecrecover(ethHash, v, r, s) == attester, "Bad sig");

        consumed[nonce] = true;
        if (activeGateMet)    _setBit(activeDayBits,    worker, day);
        if (volunteerGateMet) _setBit(volunteerDayBits, worker, day);
        emit DayAttested(worker, day, activeGateMet, volunteerGateMet);
    }

    // ─────────────────────────────────────────────────────────
    // 룰 #1 Ragequit — 이벤트 발생 + 모든 흔적 소각
    // ─────────────────────────────────────────────────────────

    /**
     * @notice "너는 언제든 떠날 자유가 있다." kWR 송금은 *없음*.
     * @dev    이벤트 RagequitRequest 발생 → zion1.top watcher가 Zion에서 unlock 실행.
     *         시민 상태는 즉시 삭제 — 봉사로 얻은 자본 보너스(lockedKWR에 안 들어감)는 소멸.
     *         재가입은 attestedPoP가 살아있으므로 join() 즉시 가능 (PoP 재증명 불필요).
     */
    function ragequit() external {
        require(citizens[msg.sender].isCitizen, "Not a citizen");
        uint256 amount = citizens[msg.sender].lockedKWR;

        delete hasBadge[msg.sender][Badge.GOVERNANCE];
        delete hasBadge[msg.sender][Badge.AUDIT];
        delete hasBadge[msg.sender][Badge.DISPUTE];
        delete hasBadge[msg.sender][Badge.NODE];
        delete citizens[msg.sender];

        emit RagequitRequest(msg.sender, amount);
    }

    // ─────────────────────────────────────────────────────────
    // 거버넌스 — 휘장 + 신탁 회전
    // ─────────────────────────────────────────────────────────

    function grantBadge(address citizen, Badge badge) external onlyGovernance {
        require(citizens[citizen].isCitizen, "Not a citizen");
        hasBadge[citizen][badge] = true;
        emit BadgeGranted(citizen, badge);
    }

    function revokeBadge(address citizen, Badge badge) external onlyGovernance {
        hasBadge[citizen][badge] = false;
        emit BadgeRevoked(citizen, badge);
    }

    function rotateAttester(address newAttester) external onlyGovernance {
        require(newAttester != address(0), "Zero attester");
        attester = newAttester;
        emit AttesterRotated(newAttester);
    }

    // ─────────────────────────────────────────────────────────
    // 핵심 내부 — 감쇠 + 주간 봉사 정산
    // ─────────────────────────────────────────────────────────

    function _applyDecay(address who, Axis axis) internal {
        Citizen storage c = citizens[who];
        uint64 today = uint64(block.timestamp / SECONDS_PER_DAY);
        uint8 axisIdx = uint8(axis);
        uint64 last = c.lastDecayDay[axisIdx];

        if (today > last) {
            uint64 dayCount = today - last;
            if (dayCount > MAX_DECAY_DAYS_PER_CALL) dayCount = MAX_DECAY_DAYS_PER_CALL;
            uint64 endDay = last + dayCount;

            uint256 honor = c.baseHonor[axisIdx];
            uint256 rate  = c.decayRateAtLastDay[axisIdx];
            uint8 sabbathOff = c.sabbathOffset;
            uint64 joinDay_  = c.joinDay;

            for (uint64 d = last + 1; d <= endDay; d++) {
                rate = (rate * DECAY_GROWTH_NUM) / DECAY_GROWTH_DEN;
                if (rate > BASIS_POINTS) rate = BASIS_POINTS;

                if (honor == 0) continue;
                if (uint8((d - joinDay_) % 7) == sabbathOff) continue;
                if (_getBit(activeDayBits, who, d)) continue;

                if (rate >= BASIS_POINTS) { honor = 0; continue; }
                honor = (honor * (BASIS_POINTS - rate)) / BASIS_POINTS;
            }

            c.baseHonor[axisIdx]          = honor;
            c.lastDecayDay[axisIdx]       = endDay;
            c.decayRateAtLastDay[axisIdx] = rate;
        }

        _processWeeks(who, c.lastDecayDay[axisIdx]);
    }

    /**
     * @dev v3.1: 자격 주마다 (a) multBP × 1.01342 + (b) 자본 += max(100, C/100).
     */
    function _processWeeks(address who, uint64 throughDay) internal {
        if (throughDay < 6) return;
        Citizen storage cit = citizens[who];
        uint64 lastCompleteWeek = (throughDay - 6) / 7;
        uint64 lastProc = cit.lastProcessedWeek;
        if (lastCompleteWeek <= lastProc) return;

        uint256 multBP = cit.volunteerMultBP;
        uint64 weeks_ = cit.volunteerWeeks;

        for (uint64 w = lastProc + 1; w <= lastCompleteWeek; w++) {
            uint64 weekStart = w * 7;
            bool qualified = false;
            for (uint8 i = 0; i < 7; i++) {
                if (_getBit(volunteerDayBits, who, weekStart + i)) {
                    qualified = true;
                    break;
                }
            }
            if (qualified) {
                weeks_++;
                multBP = (multBP * VOLUNTEER_BONUS_NUM) / VOLUNTEER_BONUS_DEN;

                // v3.1: 자본 보너스 max(VOLUNTEER_CAPITAL_MIN, C × 1%)
                uint256 capHonor = cit.baseHonor[uint8(Axis.CAPITAL)];
                uint256 capBonus = (capHonor * VOLUNTEER_CAPITAL_PCT) / BASIS_POINTS;
                if (capBonus < VOLUNTEER_CAPITAL_MIN) capBonus = VOLUNTEER_CAPITAL_MIN;
                cit.baseHonor[uint8(Axis.CAPITAL)] = capHonor + capBonus;

                emit VolunteerWeekCounted(who, w, weeks_);
                emit VolunteerCapitalBonus(who, capBonus);
            }
        }
        cit.volunteerWeeks    = weeks_;
        cit.volunteerMultBP   = multBP;
        cit.lastProcessedWeek = lastCompleteWeek;
    }

    function _setBit(
        mapping(address => mapping(uint256 => uint256)) storage bits,
        address who, uint64 day
    ) internal {
        bits[who][uint256(day) / 256] |= (uint256(1) << (uint256(day) % 256));
    }

    function _getBit(
        mapping(address => mapping(uint256 => uint256)) storage bits,
        address who, uint64 day
    ) internal view returns (bool) {
        return (bits[who][uint256(day) / 256] & (uint256(1) << (uint256(day) % 256))) != 0;
    }

    // ─────────────────────────────────────────────────────────
    // 읽기 — 현재 명예, 의결권, 분해 뷰
    // ─────────────────────────────────────────────────────────

    function getCurrentHonor(address who, Axis axis) public view returns (uint256) {
        Citizen storage c = citizens[who];
        if (!c.isCitizen) return 0;
        uint8 axisIdx = uint8(axis);
        uint256 honor = c.baseHonor[axisIdx];
        if (honor == 0) return 0;

        uint64 today = uint64(block.timestamp / SECONDS_PER_DAY);
        uint64 last = c.lastDecayDay[axisIdx];
        if (today <= last) return honor;

        uint64 dayCount = today - last;
        if (dayCount > MAX_DECAY_DAYS_PER_CALL) dayCount = MAX_DECAY_DAYS_PER_CALL;
        uint64 endDay = last + dayCount;

        uint256 rate = c.decayRateAtLastDay[axisIdx];
        uint8 sabbathOff = c.sabbathOffset;
        uint64 joinDay_  = c.joinDay;

        for (uint64 d = last + 1; d <= endDay; d++) {
            rate = (rate * DECAY_GROWTH_NUM) / DECAY_GROWTH_DEN;
            if (rate > BASIS_POINTS) rate = BASIS_POINTS;
            if (uint8((d - joinDay_) % 7) == sabbathOff) continue;
            if (_getBit(activeDayBits, who, d)) continue;
            if (rate >= BASIS_POINTS) return 0;
            honor = (honor * (BASIS_POINTS - rate)) / BASIS_POINTS;
            if (honor == 0) return 0;
        }
        return honor;
    }

    function getVotingPower(address who) public view returns (uint256) {
        if (!citizens[who].isCitizen) return 0;
        uint256 baseVP = sqrt(getCurrentHonor(who, Axis.CAPITAL))
                       + LABOR_WEIGHT        * sqrt(getCurrentHonor(who, Axis.LABOR))
                       + VERIFICATION_WEIGHT * sqrt(getCurrentHonor(who, Axis.VERIFICATION));
        uint256 multBP = _computeCurrentMultBP(who);
        uint256 sqrtFactor = sqrt(multBP * BASIS_POINTS);
        return (baseVP * sqrtFactor) / BASIS_POINTS;
    }

    function _computeCurrentMultBP(address who) internal view returns (uint256) {
        Citizen storage c = citizens[who];
        uint256 multBP = c.volunteerMultBP;
        uint64 today = uint64(block.timestamp / SECONDS_PER_DAY);
        if (today < 6) return multBP;
        uint64 lastCompleteWeek = (today - 6) / 7;
        uint64 lastProc = c.lastProcessedWeek;
        if (lastCompleteWeek <= lastProc) return multBP;

        for (uint64 w = lastProc + 1; w <= lastCompleteWeek; w++) {
            uint64 weekStart = w * 7;
            bool qualified = false;
            for (uint8 i = 0; i < 7; i++) {
                if (_getBit(volunteerDayBits, who, weekStart + i)) {
                    qualified = true;
                    break;
                }
            }
            if (qualified) {
                multBP = (multBP * VOLUNTEER_BONUS_NUM) / VOLUNTEER_BONUS_DEN;
            }
        }
        return multBP;
    }

    function getHonorBreakdown(address who)
        external view
        returns (
            uint256 capital,
            uint256 labor,
            uint256 verification,
            uint256 volunteerMultBP_,
            uint64  volunteerWeeks_,
            uint64  joinDay_,
            uint8   sabbathOffset_,
            uint256 lockedKWR_,
            uint256 votingPower,
            bool    poPVerified
        )
    {
        capital      = getCurrentHonor(who, Axis.CAPITAL);
        labor        = getCurrentHonor(who, Axis.LABOR);
        verification = getCurrentHonor(who, Axis.VERIFICATION);
        Citizen storage c = citizens[who];
        volunteerMultBP_ = _computeCurrentMultBP(who);
        volunteerWeeks_  = c.volunteerWeeks;
        joinDay_         = c.joinDay;
        sabbathOffset_   = c.sabbathOffset;
        lockedKWR_       = c.lockedKWR;
        votingPower      = getVotingPower(who);
        poPVerified      = attestedPoP[who];
    }

    // ─────────────────────────────────────────────────────────
    // 원시 수학 — Newton 정수 sqrt (스탠더드 ① 정합, 결정 #98)
    // ─────────────────────────────────────────────────────────

    function sqrt(uint256 y) internal pure returns (uint256 z) {
        if (y > 3) {
            z = y;
            uint256 x = y / 2 + 1;
            while (x < z) { z = x; x = (y / x + x) / 2; }
        } else if (y != 0) { z = 1; }
    }

    // ─── 안전망 — ETH 수신 차단 (v3.1: kWR 전용 컨트랙트, ETH 보관 안 함) ───
    receive() external payable { revert("ETH not accepted; use bridgeMintCapital"); }
    fallback() external payable { revert("No fallback"); }
}
