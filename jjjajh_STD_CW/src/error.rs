use cosmwasm_std::{OverflowError, StdError};
use thiserror::Error;

#[derive(Error, Debug, PartialEq)]
pub enum ContractError {
    #[error("{0}")]
    Std(#[from] StdError),

    #[error("{0}")]
    Overflow(#[from] OverflowError),

    #[error("Only governance can call this")]
    OnlyGovernance,

    #[error("Only attester (zion1.top) can call this")]
    OnlyAttester,

    #[error("Zion PoP verification required for this address")]
    PoPRequired,

    #[error("Already a citizen")]
    AlreadyCitizen,

    #[error("Not a citizen")]
    NotCitizen,

    #[error("Capital must be sent via funds with denom utrg")]
    InvalidCapitalDenom,

    #[error("Capital amount must be > 0")]
    ZeroCapital,

    #[error("Replay: id already consumed")]
    Replay,

    #[error("Expired")]
    Expired,

    #[error("Day must not be in the future")]
    FutureDay,

    #[error("Day must be on or after join")]
    BeforeJoin,

    #[error("Sabbath offset must be 0..6")]
    InvalidSabbathOffset,

    #[error("Capital axis uses contribute_capital, not attest_honor")]
    CapitalNotAttestable,

    #[error("Invalid attester address")]
    InvalidAttester,

    #[error("Bank send failed")]
    BankSendFailed,
}
