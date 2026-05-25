use cosmwasm_std::{
    entry_point, to_json_binary, Addr, BankMsg, Binary, Coin, Deps, DepsMut, Env, MessageInfo,
    Response, StdResult, Uint128,
};

use crate::error::ContractError;
use crate::msg::{
    Axis, Badge, CitizenBreakdownResponse, ConfigResponse, ExecuteMsg, InstantiateMsg, QueryMsg,
};
use crate::state::{
    Citizen, Config, ACTIVE_DAY_BITS, ATTESTED_POP, BASE_DECAY_BP, BASIS_POINTS, CITIZENS,
    CONFIG, CONSUMED, DECAY_GROWTH_DEN, DECAY_GROWTH_NUM, DEFAULT_SABBATH_OFFSET, HAS_BADGE,
    INITIAL_CAPITAL_HONOR, LABOR_WEIGHT, MAX_DECAY_DAYS_PER_CALL, SECONDS_PER_DAY,
    VERIFICATION_WEIGHT, VOLUNTEER_BONUS_DEN, VOLUNTEER_BONUS_NUM, VOLUNTEER_CAPITAL_MIN,
    VOLUNTEER_CAPITAL_PCT, VOLUNTEER_DAY_BITS,
};

// ─── Instantiate ───
#[entry_point]
pub fn instantiate(
    deps: DepsMut,
    _env: Env,
    _info: MessageInfo,
    msg: InstantiateMsg,
) -> Result<Response, ContractError> {
    let attester = deps.api.addr_validate(&msg.attester)?;
    let governance = deps.api.addr_validate(&msg.governance)?;
    CONFIG.save(
        deps.storage,
        &Config {
            attester,
            governance,
            kwr_denom: msg.kwr_denom.clone(),
        },
    )?;
    Ok(Response::new()
        .add_attribute("action", "instantiate")
        .add_attribute("attester", msg.attester)
        .add_attribute("governance", msg.governance)
        .add_attribute("kwr_denom", msg.kwr_denom))
}

// ─── Execute ───
#[entry_point]
pub fn execute(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    msg: ExecuteMsg,
) -> Result<Response, ContractError> {
    match msg {
        ExecuteMsg::AttestPoP { worker } => exec_attest_pop(deps, info, worker),
        ExecuteMsg::Join {} => exec_join(deps, env, info),
        ExecuteMsg::SetSabbathDay { offset } => exec_set_sabbath_day(deps, info, offset),
        ExecuteMsg::ContributeCapital {} => exec_contribute_capital(deps, env, info),
        ExecuteMsg::AttestHonor {
            worker,
            axis,
            honor_delta,
            job_id,
        } => exec_attest_honor(deps, env, info, worker, axis, honor_delta, job_id),
        ExecuteMsg::AttestDay {
            worker,
            day,
            active_gate_met,
            volunteer_gate_met,
            nonce,
        } => exec_attest_day(deps, env, info, worker, day, active_gate_met, volunteer_gate_met, nonce),
        ExecuteMsg::Ragequit {} => exec_ragequit(deps, info),
        ExecuteMsg::GrantBadge { citizen, badge } => exec_grant_badge(deps, info, citizen, badge),
        ExecuteMsg::RevokeBadge { citizen, badge } => exec_revoke_badge(deps, info, citizen, badge),
        ExecuteMsg::RotateAttester { new_attester } => exec_rotate_attester(deps, info, new_attester),
    }
}

// ─── PoP ───
fn exec_attest_pop(deps: DepsMut, info: MessageInfo, worker: String) -> Result<Response, ContractError> {
    let cfg = CONFIG.load(deps.storage)?;
    if info.sender != cfg.attester {
        return Err(ContractError::OnlyAttester);
    }
    let worker_addr = deps.api.addr_validate(&worker)?;
    ATTESTED_POP.save(deps.storage, &worker_addr, &true)?;
    Ok(Response::new()
        .add_attribute("action", "attest_pop")
        .add_attribute("worker", worker))
}

// ─── 시민권 ───
fn exec_join(deps: DepsMut, env: Env, info: MessageInfo) -> Result<Response, ContractError> {
    let sender = info.sender.clone();
    let pop_ok = ATTESTED_POP.may_load(deps.storage, &sender)?.unwrap_or(false);
    if !pop_ok {
        return Err(ContractError::PoPRequired);
    }
    if CITIZENS.may_load(deps.storage, &sender)?.is_some() {
        return Err(ContractError::AlreadyCitizen);
    }
    let today = env.block.time.seconds() / SECONDS_PER_DAY;
    let mut c = Citizen::default();
    c.is_citizen = true;
    c.join_day = today;
    c.sabbath_offset = DEFAULT_SABBATH_OFFSET;
    c.base_honor[0] = Uint128::new(INITIAL_CAPITAL_HONOR);
    c.last_decay_day = [today; 3];
    c.decay_rate_at_last_day = [Uint128::new(BASE_DECAY_BP); 3];
    c.last_processed_week = today / 7;
    c.volunteer_mult_bp = Uint128::new(BASIS_POINTS);
    CITIZENS.save(deps.storage, &sender, &c)?;
    Ok(Response::new()
        .add_attribute("action", "join")
        .add_attribute("citizen", sender.to_string())
        .add_attribute("join_day", today.to_string()))
}

fn exec_set_sabbath_day(
    deps: DepsMut,
    info: MessageInfo,
    offset: u8,
) -> Result<Response, ContractError> {
    if offset >= 7 {
        return Err(ContractError::InvalidSabbathOffset);
    }
    let mut c = CITIZENS
        .load(deps.storage, &info.sender)
        .map_err(|_| ContractError::NotCitizen)?;
    c.sabbath_offset = offset;
    CITIZENS.save(deps.storage, &info.sender, &c)?;
    Ok(Response::new()
        .add_attribute("action", "set_sabbath_day")
        .add_attribute("offset", offset.to_string()))
}

// ─── 자본 명예 — 네이티브 kWR 입금 ───
fn exec_contribute_capital(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
) -> Result<Response, ContractError> {
    let cfg = CONFIG.load(deps.storage)?;
    let pop_ok = ATTESTED_POP.may_load(deps.storage, &info.sender)?.unwrap_or(false);
    if !pop_ok {
        return Err(ContractError::PoPRequired);
    }
    let mut c = CITIZENS
        .may_load(deps.storage, &info.sender)?
        .ok_or(ContractError::NotCitizen)?;
    if !c.is_citizen {
        return Err(ContractError::NotCitizen);
    }
    // funds 검사 — 정확히 kwr_denom 하나만
    let coin = info
        .funds
        .iter()
        .find(|c| c.denom == cfg.kwr_denom)
        .ok_or(ContractError::InvalidCapitalDenom)?;
    if coin.amount.is_zero() {
        return Err(ContractError::ZeroCapital);
    }
    apply_decay(&mut c, env.block.time.seconds(), 0, &info.sender, &deps.as_ref())?;
    c.base_honor[0] = c.base_honor[0].checked_add(coin.amount)?;
    c.locked_kwr = c.locked_kwr.checked_add(coin.amount)?;
    process_weeks_with_storage(&mut c, &info.sender, deps.storage)?;
    CITIZENS.save(deps.storage, &info.sender, &c)?;
    Ok(Response::new()
        .add_attribute("action", "contribute_capital")
        .add_attribute("citizen", info.sender.to_string())
        .add_attribute("amount", coin.amount.to_string()))
}

// ─── 노동/검증 명예 ───
fn exec_attest_honor(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    worker: String,
    axis: Axis,
    honor_delta: Uint128,
    job_id: String,
) -> Result<Response, ContractError> {
    let cfg = CONFIG.load(deps.storage)?;
    if info.sender != cfg.attester {
        return Err(ContractError::OnlyAttester);
    }
    if matches!(axis, Axis::Capital) {
        return Err(ContractError::CapitalNotAttestable);
    }
    if CONSUMED.may_load(deps.storage, &job_id)?.unwrap_or(false) {
        return Err(ContractError::Replay);
    }
    let worker_addr = deps.api.addr_validate(&worker)?;
    let pop_ok = ATTESTED_POP.may_load(deps.storage, &worker_addr)?.unwrap_or(false);
    if !pop_ok {
        return Err(ContractError::PoPRequired);
    }
    let mut c = CITIZENS
        .may_load(deps.storage, &worker_addr)?
        .ok_or(ContractError::NotCitizen)?;
    let axis_idx = match axis {
        Axis::Capital => 0,
        Axis::Labor => 1,
        Axis::Verification => 2,
    };
    apply_decay(&mut c, env.block.time.seconds(), axis_idx, &worker_addr, &deps.as_ref())?;
    c.base_honor[axis_idx] = c.base_honor[axis_idx].checked_add(honor_delta)?;
    process_weeks_with_storage(&mut c, &worker_addr, deps.storage)?;
    CITIZENS.save(deps.storage, &worker_addr, &c)?;
    CONSUMED.save(deps.storage, &job_id, &true)?;
    Ok(Response::new()
        .add_attribute("action", "attest_honor")
        .add_attribute("worker", worker)
        .add_attribute("axis", format!("{:?}", axis))
        .add_attribute("honor_delta", honor_delta.to_string())
        .add_attribute("job_id", job_id))
}

// ─── 일별 게이트 ───
fn exec_attest_day(
    deps: DepsMut,
    env: Env,
    info: MessageInfo,
    worker: String,
    day: u64,
    active_gate_met: bool,
    volunteer_gate_met: bool,
    nonce: String,
) -> Result<Response, ContractError> {
    let cfg = CONFIG.load(deps.storage)?;
    if info.sender != cfg.attester {
        return Err(ContractError::OnlyAttester);
    }
    if CONSUMED.may_load(deps.storage, &nonce)?.unwrap_or(false) {
        return Err(ContractError::Replay);
    }
    let worker_addr = deps.api.addr_validate(&worker)?;
    let pop_ok = ATTESTED_POP.may_load(deps.storage, &worker_addr)?.unwrap_or(false);
    if !pop_ok {
        return Err(ContractError::PoPRequired);
    }
    let c = CITIZENS
        .may_load(deps.storage, &worker_addr)?
        .ok_or(ContractError::NotCitizen)?;
    let today = env.block.time.seconds() / SECONDS_PER_DAY;
    if day > today {
        return Err(ContractError::FutureDay);
    }
    if day < c.join_day {
        return Err(ContractError::BeforeJoin);
    }
    CONSUMED.save(deps.storage, &nonce, &true)?;
    if active_gate_met {
        set_bit(deps.storage, &ACTIVE_DAY_BITS, &worker_addr, day)?;
    }
    if volunteer_gate_met {
        set_bit(deps.storage, &VOLUNTEER_DAY_BITS, &worker_addr, day)?;
    }
    Ok(Response::new()
        .add_attribute("action", "attest_day")
        .add_attribute("worker", worker)
        .add_attribute("day", day.to_string())
        .add_attribute("active", active_gate_met.to_string())
        .add_attribute("volunteer", volunteer_gate_met.to_string()))
}

// ─── Ragequit — bank send + 상태 삭제 ───
fn exec_ragequit(deps: DepsMut, info: MessageInfo) -> Result<Response, ContractError> {
    let cfg = CONFIG.load(deps.storage)?;
    let c = CITIZENS
        .may_load(deps.storage, &info.sender)?
        .ok_or(ContractError::NotCitizen)?;
    let amount = c.locked_kwr;

    // 휘장 + 시민 상태 삭제
    for badge_idx in 0u8..4u8 {
        HAS_BADGE.remove(deps.storage, (&info.sender, badge_idx));
    }
    CITIZENS.remove(deps.storage, &info.sender);
    // 비트맵은 그대로 둠 — 재가입 시 새 join_day가 lastDecayDay/lastProcessedWeek 결정

    let mut resp = Response::new()
        .add_attribute("action", "ragequit")
        .add_attribute("citizen", info.sender.to_string())
        .add_attribute("amount", amount.to_string());
    if !amount.is_zero() {
        resp = resp.add_message(BankMsg::Send {
            to_address: info.sender.to_string(),
            amount: vec![Coin {
                denom: cfg.kwr_denom,
                amount,
            }],
        });
    }
    Ok(resp)
}

// ─── 거버넌스 ───
fn exec_grant_badge(
    deps: DepsMut,
    info: MessageInfo,
    citizen: String,
    badge: Badge,
) -> Result<Response, ContractError> {
    let cfg = CONFIG.load(deps.storage)?;
    if info.sender != cfg.governance {
        return Err(ContractError::OnlyGovernance);
    }
    let cit_addr = deps.api.addr_validate(&citizen)?;
    if CITIZENS.may_load(deps.storage, &cit_addr)?.is_none() {
        return Err(ContractError::NotCitizen);
    }
    HAS_BADGE.save(deps.storage, (&cit_addr, badge_to_u8(&badge)), &true)?;
    Ok(Response::new()
        .add_attribute("action", "grant_badge")
        .add_attribute("citizen", citizen)
        .add_attribute("badge", format!("{:?}", badge)))
}

fn exec_revoke_badge(
    deps: DepsMut,
    info: MessageInfo,
    citizen: String,
    badge: Badge,
) -> Result<Response, ContractError> {
    let cfg = CONFIG.load(deps.storage)?;
    if info.sender != cfg.governance {
        return Err(ContractError::OnlyGovernance);
    }
    let cit_addr = deps.api.addr_validate(&citizen)?;
    HAS_BADGE.save(deps.storage, (&cit_addr, badge_to_u8(&badge)), &false)?;
    Ok(Response::new()
        .add_attribute("action", "revoke_badge")
        .add_attribute("citizen", citizen))
}

fn exec_rotate_attester(
    deps: DepsMut,
    info: MessageInfo,
    new_attester: String,
) -> Result<Response, ContractError> {
    let mut cfg = CONFIG.load(deps.storage)?;
    if info.sender != cfg.governance {
        return Err(ContractError::OnlyGovernance);
    }
    let new_addr = deps.api.addr_validate(&new_attester)?;
    cfg.attester = new_addr;
    CONFIG.save(deps.storage, &cfg)?;
    Ok(Response::new()
        .add_attribute("action", "rotate_attester")
        .add_attribute("new_attester", new_attester))
}

fn badge_to_u8(b: &Badge) -> u8 {
    match b {
        Badge::Governance => 0,
        Badge::Audit => 1,
        Badge::Dispute => 2,
        Badge::Node => 3,
    }
}

// ─── 비트맵 helper ───
fn set_bit(
    storage: &mut dyn cosmwasm_std::Storage,
    map: &cw_storage_plus::Map<(&Addr, u64), u128>,
    who: &Addr,
    day: u64,
) -> StdResult<()> {
    let word_idx = day / 128;
    let bit_pos = day % 128;
    let current = map.may_load(storage, (who, word_idx))?.unwrap_or(0u128);
    let new_val = current | (1u128 << bit_pos);
    map.save(storage, (who, word_idx), &new_val)?;
    Ok(())
}

fn get_bit(
    deps: &Deps,
    map: &cw_storage_plus::Map<(&Addr, u64), u128>,
    who: &Addr,
    day: u64,
) -> bool {
    let word_idx = day / 128;
    let bit_pos = day % 128;
    let current = map.may_load(deps.storage, (who, word_idx)).unwrap_or(Some(0u128)).unwrap_or(0u128);
    (current & (1u128 << bit_pos)) != 0
}

// ─── 핵심 로직 — 감쇠 적용 ───
fn apply_decay(
    c: &mut Citizen,
    now_seconds: u64,
    axis_idx: usize,
    who: &Addr,
    deps: &Deps,
) -> Result<(), ContractError> {
    let today = now_seconds / SECONDS_PER_DAY;
    let last = c.last_decay_day[axis_idx];
    if today <= last {
        return Ok(());
    }
    let mut day_count = today - last;
    if day_count > MAX_DECAY_DAYS_PER_CALL {
        day_count = MAX_DECAY_DAYS_PER_CALL;
    }
    let end_day = last + day_count;

    let mut honor = c.base_honor[axis_idx].u128();
    let mut rate = c.decay_rate_at_last_day[axis_idx].u128();
    let sabbath_off = c.sabbath_offset as u64;
    let join_day = c.join_day;

    let mut d = last + 1;
    while d <= end_day {
        rate = rate * DECAY_GROWTH_NUM / DECAY_GROWTH_DEN;
        if rate > BASIS_POINTS {
            rate = BASIS_POINTS;
        }

        if honor == 0 {
            d += 1;
            continue;
        }

        let cycle_idx = (d - join_day) % 7;
        if cycle_idx == sabbath_off {
            d += 1;
            continue;
        }
        if get_bit(deps, &ACTIVE_DAY_BITS, who, d) {
            d += 1;
            continue;
        }

        if rate >= BASIS_POINTS {
            honor = 0;
        } else {
            honor = honor * (BASIS_POINTS - rate) / BASIS_POINTS;
        }
        d += 1;
    }

    c.base_honor[axis_idx] = Uint128::new(honor);
    c.last_decay_day[axis_idx] = end_day;
    c.decay_rate_at_last_day[axis_idx] = Uint128::new(rate);
    Ok(())
}

// ─── 주간 봉사 정산 — 자격 주마다 multBP + 자본 보너스 ───
fn process_weeks_with_storage(
    c: &mut Citizen,
    who: &Addr,
    storage: &mut dyn cosmwasm_std::Storage,
) -> Result<(), ContractError> {
    // through_day = max of all 3 axes' last_decay_day
    let through_day = *c.last_decay_day.iter().max().unwrap();
    if through_day < 6 {
        return Ok(());
    }
    let last_complete_week = (through_day - 6) / 7;
    if last_complete_week <= c.last_processed_week {
        return Ok(());
    }

    let mut mult_bp = c.volunteer_mult_bp.u128();
    let mut weeks_ = c.volunteer_weeks;

    let mut w = c.last_processed_week + 1;
    while w <= last_complete_week {
        let week_start = w * 7;
        let mut qualified = false;
        for i in 0u64..7u64 {
            let day = week_start + i;
            let word_idx = day / 128;
            let bit_pos = day % 128;
            let current = VOLUNTEER_DAY_BITS
                .may_load(storage, (who, word_idx))?
                .unwrap_or(0u128);
            if (current & (1u128 << bit_pos)) != 0 {
                qualified = true;
                break;
            }
        }
        if qualified {
            weeks_ += 1;
            mult_bp = mult_bp * VOLUNTEER_BONUS_NUM / VOLUNTEER_BONUS_DEN;
            // v3.1: 자본 보너스 max(VOLUNTEER_CAPITAL_MIN, C × 1%)
            let cap_honor = c.base_honor[0].u128();
            let mut cap_bonus = cap_honor * VOLUNTEER_CAPITAL_PCT / BASIS_POINTS;
            if cap_bonus < VOLUNTEER_CAPITAL_MIN {
                cap_bonus = VOLUNTEER_CAPITAL_MIN;
            }
            c.base_honor[0] = Uint128::new(cap_honor + cap_bonus);
        }
        w += 1;
    }
    c.volunteer_weeks = weeks_;
    c.volunteer_mult_bp = Uint128::new(mult_bp);
    c.last_processed_week = last_complete_week;
    Ok(())
}

// ─── 정수 sqrt (Newton 바빌로니아, Solidity와 동일) ───
fn int_sqrt(y: u128) -> u128 {
    if y == 0 {
        return 0;
    }
    if y <= 3 {
        return 1;
    }
    let mut z = y;
    let mut x = y / 2 + 1;
    while x < z {
        z = x;
        x = (y / x + x) / 2;
    }
    z
}

// ─── Query ───
#[entry_point]
pub fn query(deps: Deps, env: Env, msg: QueryMsg) -> StdResult<Binary> {
    match msg {
        QueryMsg::HonorBreakdown { who } => to_json_binary(&query_breakdown(deps, env, who)?),
        QueryMsg::CurrentHonor { who, axis } => to_json_binary(&query_current_honor(deps, env, who, axis)?),
        QueryMsg::VotingPower { who } => to_json_binary(&query_voting_power(deps, env, who)?),
        QueryMsg::Config {} => to_json_binary(&query_config(deps)?),
        QueryMsg::IsPoPAttested { who } => to_json_binary(&query_pop(deps, who)?),
    }
}

fn query_config(deps: Deps) -> StdResult<ConfigResponse> {
    let cfg = CONFIG.load(deps.storage)?;
    Ok(ConfigResponse {
        attester: cfg.attester.to_string(),
        governance: cfg.governance.to_string(),
        kwr_denom: cfg.kwr_denom,
    })
}

fn query_pop(deps: Deps, who: String) -> StdResult<bool> {
    let addr = deps.api.addr_validate(&who)?;
    Ok(ATTESTED_POP.may_load(deps.storage, &addr)?.unwrap_or(false))
}

fn query_current_honor(deps: Deps, env: Env, who: String, axis: Axis) -> StdResult<Uint128> {
    let addr = deps.api.addr_validate(&who)?;
    let c = match CITIZENS.may_load(deps.storage, &addr)? {
        Some(c) => c,
        None => return Ok(Uint128::zero()),
    };
    let axis_idx = match axis {
        Axis::Capital => 0,
        Axis::Labor => 1,
        Axis::Verification => 2,
    };
    Ok(Uint128::new(simulate_current_honor(&c, &deps, &addr, env.block.time.seconds(), axis_idx)))
}

fn simulate_current_honor(c: &Citizen, deps: &Deps, who: &Addr, now: u64, axis_idx: usize) -> u128 {
    let mut honor = c.base_honor[axis_idx].u128();
    if honor == 0 || !c.is_citizen {
        return 0;
    }
    let today = now / SECONDS_PER_DAY;
    let last = c.last_decay_day[axis_idx];
    if today <= last {
        return honor;
    }
    let mut day_count = today - last;
    if day_count > MAX_DECAY_DAYS_PER_CALL {
        day_count = MAX_DECAY_DAYS_PER_CALL;
    }
    let end_day = last + day_count;
    let mut rate = c.decay_rate_at_last_day[axis_idx].u128();
    let sabbath_off = c.sabbath_offset as u64;
    let join_day = c.join_day;
    let mut d = last + 1;
    while d <= end_day {
        rate = rate * DECAY_GROWTH_NUM / DECAY_GROWTH_DEN;
        if rate > BASIS_POINTS {
            rate = BASIS_POINTS;
        }
        let cycle_idx = (d - join_day) % 7;
        if cycle_idx != sabbath_off && !get_bit(deps, &ACTIVE_DAY_BITS, who, d) {
            if rate >= BASIS_POINTS {
                return 0;
            }
            honor = honor * (BASIS_POINTS - rate) / BASIS_POINTS;
            if honor == 0 {
                return 0;
            }
        }
        d += 1;
    }
    honor
}

fn simulate_current_mult_bp(c: &Citizen, deps: &Deps, who: &Addr, now: u64) -> u128 {
    let mut mult_bp = c.volunteer_mult_bp.u128();
    let today = now / SECONDS_PER_DAY;
    if today < 6 {
        return mult_bp;
    }
    let last_complete_week = (today - 6) / 7;
    if last_complete_week <= c.last_processed_week {
        return mult_bp;
    }
    let mut w = c.last_processed_week + 1;
    while w <= last_complete_week {
        let week_start = w * 7;
        let mut qualified = false;
        for i in 0u64..7u64 {
            if get_bit(deps, &VOLUNTEER_DAY_BITS, who, week_start + i) {
                qualified = true;
                break;
            }
        }
        if qualified {
            mult_bp = mult_bp * VOLUNTEER_BONUS_NUM / VOLUNTEER_BONUS_DEN;
        }
        w += 1;
    }
    mult_bp
}

fn query_voting_power(deps: Deps, env: Env, who: String) -> StdResult<Uint128> {
    let addr = deps.api.addr_validate(&who)?;
    let c = match CITIZENS.may_load(deps.storage, &addr)? {
        Some(c) => c,
        None => return Ok(Uint128::zero()),
    };
    if !c.is_citizen {
        return Ok(Uint128::zero());
    }
    let now = env.block.time.seconds();
    let c_h = simulate_current_honor(&c, &deps, &addr, now, 0);
    let l_h = simulate_current_honor(&c, &deps, &addr, now, 1);
    let v_h = simulate_current_honor(&c, &deps, &addr, now, 2);
    let base_vp = int_sqrt(c_h)
        + LABOR_WEIGHT * int_sqrt(l_h)
        + VERIFICATION_WEIGHT * int_sqrt(v_h);
    let mult_bp = simulate_current_mult_bp(&c, &deps, &addr, now);
    let sqrt_factor = int_sqrt(mult_bp * BASIS_POINTS);
    Ok(Uint128::new(base_vp * sqrt_factor / BASIS_POINTS))
}

fn query_breakdown(deps: Deps, env: Env, who: String) -> StdResult<CitizenBreakdownResponse> {
    let addr = deps.api.addr_validate(&who)?;
    let c = CITIZENS.may_load(deps.storage, &addr)?.unwrap_or_default();
    let now = env.block.time.seconds();
    let pop = ATTESTED_POP.may_load(deps.storage, &addr)?.unwrap_or(false);
    let cap = simulate_current_honor(&c, &deps, &addr, now, 0);
    let lab = simulate_current_honor(&c, &deps, &addr, now, 1);
    let ver = simulate_current_honor(&c, &deps, &addr, now, 2);
    let mult = simulate_current_mult_bp(&c, &deps, &addr, now);
    let base_vp = int_sqrt(cap) + LABOR_WEIGHT * int_sqrt(lab) + VERIFICATION_WEIGHT * int_sqrt(ver);
    let sqrt_factor = int_sqrt(mult * BASIS_POINTS);
    let vp = base_vp * sqrt_factor / BASIS_POINTS;
    Ok(CitizenBreakdownResponse {
        is_citizen: c.is_citizen,
        join_day: c.join_day,
        sabbath_offset: c.sabbath_offset,
        capital: Uint128::new(cap),
        labor: Uint128::new(lab),
        verification: Uint128::new(ver),
        volunteer_weeks: c.volunteer_weeks,
        volunteer_mult_bp: Uint128::new(mult),
        locked_kwr: c.locked_kwr,
        voting_power: Uint128::new(vp),
        pop_verified: pop,
    })
}
