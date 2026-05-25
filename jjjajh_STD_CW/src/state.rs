use cosmwasm_schema::cw_serde;
use cosmwasm_std::{Addr, Uint128};
use cw_storage_plus::{Item, Map};

// ─── 정수 결정론 상수 (Solidity v3.1과 동일) ───
pub const BASIS_POINTS: u128            = 10_000;
pub const BASE_DECAY_BP: u128           = 100;     // 1.0%
pub const DECAY_GROWTH_NUM: u128        = 101;
pub const DECAY_GROWTH_DEN: u128        = 100;
pub const VOLUNTEER_BONUS_NUM: u128     = 101_342;
pub const VOLUNTEER_BONUS_DEN: u128     = 100_000;
pub const VOLUNTEER_CAPITAL_MIN: u128   = 100;     // 봉사 시 최소 자본 +100 kWR
pub const VOLUNTEER_CAPITAL_PCT: u128   = 100;     // 1% (BP form)
pub const SECONDS_PER_DAY: u64          = 86_400;
pub const MAX_DECAY_DAYS_PER_CALL: u64  = 90;
pub const DEFAULT_SABBATH_OFFSET: u8    = 6;
pub const INITIAL_CAPITAL_HONOR: u128   = 1;
pub const LABOR_WEIGHT: u128            = 5;
pub const VERIFICATION_WEIGHT: u128     = 5;

#[cw_serde]
pub struct Config {
    pub attester: Addr,
    pub governance: Addr,
    pub kwr_denom: String,
}

#[cw_serde]
pub struct Citizen {
    pub is_citizen: bool,
    pub join_day: u64,
    pub sabbath_offset: u8,
    pub locked_kwr: Uint128,             // ragequit 시 반환 대상 (kWR utrg)
    pub base_honor: [Uint128; 3],        // [Capital, Labor, Verification]
    pub last_decay_day: [u64; 3],
    pub decay_rate_at_last_day: [Uint128; 3],
    pub volunteer_weeks: u64,
    pub last_processed_week: u64,
    pub volunteer_mult_bp: Uint128,
}

impl Default for Citizen {
    fn default() -> Self {
        Self {
            is_citizen: false,
            join_day: 0,
            sabbath_offset: DEFAULT_SABBATH_OFFSET,
            locked_kwr: Uint128::zero(),
            base_honor: [Uint128::zero(); 3],
            last_decay_day: [0; 3],
            decay_rate_at_last_day: [Uint128::zero(); 3],
            volunteer_weeks: 0,
            last_processed_week: 0,
            volunteer_mult_bp: Uint128::new(BASIS_POINTS),
        }
    }
}

pub const CONFIG: Item<Config> = Item::new("config");
pub const CITIZENS: Map<&Addr, Citizen> = Map::new("citizens");
pub const ATTESTED_POP: Map<&Addr, bool> = Map::new("attested_pop");
pub const HAS_BADGE: Map<(&Addr, u8), bool> = Map::new("has_badge");

// 비트맵: word packing (256 days per u128 — 단 u128은 128 bit, so 128 days/slot)
// (Solidity는 uint256=256 bit. CW에선 u128 사용, 2슬롯/256일)
// 키: (citizen_addr, word_idx)
pub const ACTIVE_DAY_BITS: Map<(&Addr, u64), u128> = Map::new("active_day_bits");
pub const VOLUNTEER_DAY_BITS: Map<(&Addr, u64), u128> = Map::new("volunteer_day_bits");

// 리플레이 방지 — 통합 namespace
pub const CONSUMED: Map<&str, bool> = Map::new("consumed");
