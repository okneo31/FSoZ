use cosmwasm_schema::{cw_serde, QueryResponses};
use cosmwasm_std::Uint128;

#[cw_serde]
pub struct InstantiateMsg {
    pub attester: String,        // zion1.top 주소 (zion1...)
    pub governance: String,      // 거버넌스 주소 (변경 가능 — rotate_attester / grant_badge)
    pub kwr_denom: String,       // 보통 "utrg"
}

#[cw_serde]
pub enum ExecuteMsg {
    // PoP — attester가 Zion x/pop 통과 사실 등록
    AttestPoP { worker: String },

    // 시민권
    Join {},
    SetSabbathDay { offset: u8 },

    // 자본 명예 — 사용자가 funds (utrg)와 함께 호출. lockedKWR 누적.
    ContributeCapital {},

    // 노동/검증 명예 — attester만
    AttestHonor {
        worker: String,
        axis: Axis,
        honor_delta: Uint128,
        job_id: String,
    },

    // 일별 게이트 — attester만
    AttestDay {
        worker: String,
        day: u64,
        active_gate_met: bool,
        volunteer_gate_met: bool,
        nonce: String,
    },

    // Ragequit — 자산 즉시 bank send + 상태 삭제
    Ragequit {},

    // 거버넌스
    GrantBadge { citizen: String, badge: Badge },
    RevokeBadge { citizen: String, badge: Badge },
    RotateAttester { new_attester: String },
}

#[cw_serde]
pub enum Axis {
    Capital,
    Labor,
    Verification,
}

#[cw_serde]
pub enum Badge {
    Governance,
    Audit,
    Dispute,
    Node,
}

#[cw_serde]
#[derive(QueryResponses)]
pub enum QueryMsg {
    #[returns(CitizenBreakdownResponse)]
    HonorBreakdown { who: String },

    #[returns(Uint128)]
    CurrentHonor { who: String, axis: Axis },

    #[returns(Uint128)]
    VotingPower { who: String },

    #[returns(ConfigResponse)]
    Config {},

    #[returns(bool)]
    IsPoPAttested { who: String },
}

#[cw_serde]
pub struct CitizenBreakdownResponse {
    pub is_citizen: bool,
    pub join_day: u64,
    pub sabbath_offset: u8,
    pub capital: Uint128,
    pub labor: Uint128,
    pub verification: Uint128,
    pub volunteer_weeks: u64,
    pub volunteer_mult_bp: Uint128,
    pub locked_kwr: Uint128,
    pub voting_power: Uint128,
    pub pop_verified: bool,
}

#[cw_serde]
pub struct ConfigResponse {
    pub attester: String,
    pub governance: String,
    pub kwr_denom: String,
}
