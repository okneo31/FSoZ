// SPDX-License-Identifier: MIT
pragma solidity ^0.8.20;

/**
 * @title SeumStandard (okneo31 안재현 스탠더드 v2)
 * @dev 스탠더드 3축 + 5 결정적 룰의 집행 컨트랙트.
 * @notice "인간을 일으켜 세우는 일, 코드가 곧 사랑"
 *
 * v2 패치 (jjjajh_STD_SC, 2026-05-23):
 *   H1: governance 접근 제어 (휘장 부여/회수)
 *   H2: contribute() — 명예 누적 + lastUpdateTime 리셋
 *   H3: 지수 감쇠 (정수 반복) — 시뮬레이터(.html)·문서(.md)와 동일 모델
 *   M1: 명예 ≠ 자본 분리 (가입 시 초기 명예 = 1, 자본 비례 아님)
 *   M2: revokeBadge — 순환 직위·임기 제한 가능
 *   M3: ragequit이 휘장도 함께 소각
 *   L1: 명시적 receive/fallback
 *   L2: CEI 패턴 유지 — 재진입 안전 (현 상태)
 */
contract SeumStandard {

    // ─── 스탠더드 ① 근사+무한 공존 — 정수 결정론 (결정 #98) ───
    uint256 public constant BASIS_POINTS    = 10000;
    uint256 public constant DECAY_RATE_BP   = 100;     // 일일 1%
    uint256 public constant MAX_DECAY_DAYS  = 10000;   // ~27년. gas 상한.
    uint256 public constant INITIAL_HONOR   = 1;       // 가입 시 명예 = 1 (자본 비례 차단)

    // ─── 거버넌스 (H1) ───
    address public governance;
    modifier onlyGovernance() { require(msg.sender == governance, "Only governance"); _; }

    // ─── 룰 #2: Soulbound 정체성 — "정체성은 매물이 아니다" ───
    struct Citizen {
        bool    isCitizen;
        uint256 depositedAssets;   // 절대적 재산권 — Ragequit 시 100% 회수
        uint256 baseHonor;         // 마지막 갱신 시점의 명예 (이후 감쇠 적용)
        uint256 lastUpdateTime;    // 감쇠 기점
    }
    mapping(address => Citizen) public citizens;

    // ─── 룰 #5: 다원 휘장 ───
    enum Badge { GOVERNANCE, AUDIT, DISPUTE, NODE }
    mapping(address => mapping(Badge => bool)) public hasBadge;

    // ─── 이벤트 ───
    event CitizenBorn(address indexed citizen);
    event Contributed(address indexed citizen, uint256 amount);
    event RagequitExecuted(address indexed citizen, uint256 recoveredAssets);
    event BadgeGranted(address indexed citizen, Badge badge);
    event BadgeRevoked(address indexed citizen, Badge badge);

    constructor() {
        governance = msg.sender;
    }

    // ─────────────────────────────────────────────────────────
    // 시민권 — 가입 + 기여
    // ─────────────────────────────────────────────────────────

    /**
     * @notice 가입 — 자산 예치 + 시민권 발급. 초기 명예는 자본과 무관 (M1).
     * @dev    명예는 오직 contribute()로만 쌓입니다. 자본 비례 독점 차단(룰 #4 정합).
     */
    function join() external payable {
        require(!citizens[msg.sender].isCitizen, "Already a citizen");
        citizens[msg.sender] = Citizen({
            isCitizen:       true,
            depositedAssets: msg.value,
            baseHonor:       INITIAL_HONOR,
            lastUpdateTime:  block.timestamp
        });
        emit CitizenBorn(msg.sender);
    }

    /**
     * @notice 기여 — 명예 누적 + 자산 추가 예치 (H2).
     * @dev    현재(감쇠된) 명예 + 새 기여 → 새 baseHonor, lastUpdateTime 리셋.
     *         "지속 기여 = 권력 유지" 룰 #3의 작동 메커니즘.
     */
    function contribute() external payable {
        require(citizens[msg.sender].isCitizen, "Not a citizen");
        require(msg.value > 0, "Zero contribution");
        uint256 current = getCurrentHonor(msg.sender);
        citizens[msg.sender].baseHonor       = current + msg.value;
        citizens[msg.sender].depositedAssets += msg.value;
        citizens[msg.sender].lastUpdateTime  = block.timestamp;
        emit Contributed(msg.sender, msg.value);
    }

    // ─────────────────────────────────────────────────────────
    // 룰 #1: Ragequit — 자산 100% 회수 + 모든 흔적 소각
    // ─────────────────────────────────────────────────────────

    /**
     * @notice "너는 언제든 떠날 자유가 있다" — 강제 동결·인질 거부.
     * @dev    CEI 패턴: state 먼저 정리, 외부 호출 나중 — 재진입 안전(L2).
     *         휘장도 함께 소각(M3) — Ragequit는 모든 흔적을 지웁니다.
     */
    function ragequit() external {
        require(citizens[msg.sender].isCitizen, "Not a citizen");
        uint256 amountToRecover = citizens[msg.sender].depositedAssets;

        // 휘장 정리 (M3)
        delete hasBadge[msg.sender][Badge.GOVERNANCE];
        delete hasBadge[msg.sender][Badge.AUDIT];
        delete hasBadge[msg.sender][Badge.DISPUTE];
        delete hasBadge[msg.sender][Badge.NODE];
        delete citizens[msg.sender];

        (bool success, ) = payable(msg.sender).call{value: amountToRecover}("");
        require(success, "Transfer failed");
        emit RagequitExecuted(msg.sender, amountToRecover);
    }

    // ─────────────────────────────────────────────────────────
    // 룰 #3: 감쇠 명예 — 지수 감쇠 (정수 반복)
    // ─────────────────────────────────────────────────────────

    /**
     * @notice "누구도 너 위에 영원히 군림 못 한다" — 0에 점근하나 닿지 않음.
     * @dev    H_t = H_0 × (BP - r)^t / BP^t — 정수 반복 곱셈으로 결정론 유지.
     *         부동소수점 0. 시뮬레이터(.html)·문서(.md)와 동일 모델 (H3 정합).
     *         gas 상한: 최대 MAX_DECAY_DAYS 반복.
     */
    function getCurrentHonor(address _citizen) public view returns (uint256) {
        Citizen memory c = citizens[_citizen];
        if (!c.isCitizen || c.baseHonor == 0) return 0;

        uint256 d = (block.timestamp - c.lastUpdateTime) / 1 days;
        if (d == 0) return c.baseHonor;
        if (d > MAX_DECAY_DAYS) d = MAX_DECAY_DAYS;

        uint256 h = c.baseHonor;
        uint256 remainBP = BASIS_POINTS - DECAY_RATE_BP;
        for (uint256 i = 0; i < d; i++) {
            h = (h * remainBP) / BASIS_POINTS;
            if (h == 0) break;
        }
        return h;
    }

    // ─────────────────────────────────────────────────────────
    // 룰 #4: Quadratic 가중치 — 정수 Newton sqrt
    // ─────────────────────────────────────────────────────────

    /**
     * @notice "큰 기여보다 많은 기여자가 이긴다" — 자본 비례 독점 차단.
     * @dev    10× 기여 → √10 ≈ 3.16× 권력. 한계효용 급감 → 카르텔 수학적 비효율화.
     */
    function getVotingPower(address _citizen) public view returns (uint256) {
        return sqrt(getCurrentHonor(_citizen));
    }

    // ─────────────────────────────────────────────────────────
    // 룰 #5: 다원 휘장 — 분야별 권한
    // ─────────────────────────────────────────────────────────

    /**
     * @notice "각자의 다른 결이 다 필요하다" — 단일 분야 독점 차단.
     * @dev    거버넌스만 부여 가능 (H1).
     */
    function grantBadge(address _citizen, Badge _badge) external onlyGovernance {
        require(citizens[_citizen].isCitizen, "Not a citizen");
        hasBadge[_citizen][_badge] = true;
        emit BadgeGranted(_citizen, _badge);
    }

    /**
     * @notice 휘장 회수 (M2) — 순환 직위·임기 제한·다양성 보너스 조정 가능.
     */
    function revokeBadge(address _citizen, Badge _badge) external onlyGovernance {
        hasBadge[_citizen][_badge] = false;
        emit BadgeRevoked(_citizen, _badge);
    }

    // ─────────────────────────────────────────────────────────
    // 원시 수학 — Newton 정수 sqrt
    // ─────────────────────────────────────────────────────────

    /**
     * @dev 바빌로니아(Newton) 정수 제곱근. 부동소수점 0.
     *      스탠더드 ①번 축 (근사+무한) + 결정 #98 (정수 전용) 정합.
     *      이 함수가 시뮬레이터의 intSqrt와 *동일 결과* — 양 매체 일관.
     */
    function sqrt(uint256 y) internal pure returns (uint256 z) {
        if (y > 3) {
            z = y;
            uint256 x = y / 2 + 1;
            while (x < z) {
                z = x;
                x = (y / x + x) / 2;
            }
        } else if (y != 0) {
            z = 1;
        }
    }

    // ─────────────────────────────────────────────────────────
    // 안전망 — 명시적 receive/fallback (L1)
    // ─────────────────────────────────────────────────────────

    receive() external payable { revert("Use join() or contribute()"); }
    fallback() external payable { revert("No fallback"); }
}
