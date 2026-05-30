// print-digests.js — Solidity 측 abi.encode + keccak256 결과를 출력.
// Go signer (zion1-daemon/internal/signer) 의 PackXxxDigest 와 비트 동일해야.
//
// 실행: node tools/print-digests.js
// 또는: pnpm hardhat run tools/print-digests.js
//
// hardhat 없이도 동작 (ethers v6는 hardhat의 deps로 이미 설치됨).

const { ethers } = require("ethers");

const abi = ethers.AbiCoder.defaultAbiCoder();

// ─── 고정 입력 (Go 테스트와 동일해야) ───
const CONTRACT = "0x1111111111111111111111111111111111111111";
const WORKER   = "0x2222222222222222222222222222222222222222";
const EXPIRY   = 1700000000n;

const NONCE_POP  = "0x" + "aa".repeat(32);
const LOCK_ID    = "0x" + "bb".repeat(32);
const JOB_ID     = "0x" + "cc".repeat(32);
const NONCE_DAY  = "0x" + "dd".repeat(32);

const KWR_AMOUNT  = 12345n;
const AXIS_LABOR  = 1; // uint8
const HONOR_DELTA = 5000n;
const DAY         = 18000n;
const ACTIVE      = true;
const VOLUNTEER   = false;

function pack(label, types, values) {
  const encoded = abi.encode(types, values);
  const digest  = ethers.keccak256(encoded);
  console.log(`${label.padEnd(20)} ${digest}`);
  return digest;
}

console.log("// 입력:");
console.log(`//   contract = ${CONTRACT}`);
console.log(`//   worker   = ${WORKER}`);
console.log(`//   expiry   = ${EXPIRY}`);
console.log(`//   nonce_pop= ${NONCE_POP}`);
console.log(`//   lockId   = ${LOCK_ID}`);
console.log(`//   jobId    = ${JOB_ID}`);
console.log(`//   nonce_day= ${NONCE_DAY}`);
console.log(`//   kwrAmount= ${KWR_AMOUNT}`);
console.log(`//   axis     = ${AXIS_LABOR}`);
console.log(`//   honorDelta=${HONOR_DELTA}`);
console.log(`//   day      = ${DAY}`);
console.log(`//   active   = ${ACTIVE}, volunteer = ${VOLUNTEER}`);
console.log("");
console.log("// expected digests (hex, 32 bytes):");

pack(
  "ATTEST_POP",
  ["string", "address", "address", "uint64", "bytes32"],
  ["ATTEST_POP", CONTRACT, WORKER, EXPIRY, NONCE_POP],
);

pack(
  "BRIDGE_MINT_CAPITAL",
  ["string", "address", "address", "uint256", "bytes32", "uint64"],
  ["BRIDGE_MINT_CAPITAL", CONTRACT, WORKER, KWR_AMOUNT, LOCK_ID, EXPIRY],
);

pack(
  "ATTEST_HONOR",
  ["string", "address", "address", "uint8", "uint256", "bytes32", "uint64"],
  ["ATTEST_HONOR", CONTRACT, WORKER, AXIS_LABOR, HONOR_DELTA, JOB_ID, EXPIRY],
);

pack(
  "ATTEST_DAY",
  ["string", "address", "address", "uint64", "bool", "bool", "uint64", "bytes32"],
  ["ATTEST_DAY", CONTRACT, WORKER, DAY, ACTIVE, VOLUNTEER, EXPIRY, NONCE_DAY],
);
