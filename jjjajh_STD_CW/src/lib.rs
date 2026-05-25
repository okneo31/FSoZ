// SeumStandard v3.1 — CosmWasm 컨트랙트 (Zion 네이티브)
// "명예는 단일 정의가 아니다. 자본·노동·검증, 셋이 다 필요하다."

pub mod contract;
pub mod error;
pub mod msg;
pub mod state;

#[cfg(test)]
mod tests;

pub use crate::error::ContractError;
