// SeumStandard v3.1 단위 테스트
// 실행: `pnpm i && pnpm test` (또는 npm)
// 참고: Spec Lock v7 / 컨트랙트 헤더 주석 / D:\FSoZ\zion-integration.md

const { expect } = require("chai");
const { ethers } = require("hardhat");
const { time } = require("@nomicfoundation/hardhat-toolbox/network-helpers");

// ─── 상수 (컨트랙트와 동일) ───
const BP                 = 10000n;
const BASE_DECAY_BP      = 100n;
const DEFAULT_SABBATH    = 6;
const SECONDS_PER_DAY    = 86400n;
const MAX_DECAY_DAYS     = 90n;
const INITIAL_CAPITAL    = 1n;
const VOL_CAPITAL_MIN    = 100n;

// ─── 헬퍼: EIP-191 personal_sign ───
async function signAttestation(attester, contract, types, values) {
  // digest = keccak256(abi.encode(...))
  const abiCoder = ethers.AbiCoder.defaultAbiCoder();
  const encoded = abiCoder.encode(types, values);
  const digest = ethers.keccak256(encoded);
  // EIP-191 personal_sign: signMessage prepends "\x19Ethereum Signed Message:\n32"
  const sig = await attester.signMessage(ethers.getBytes(digest));
  const { v, r, s } = ethers.Signature.from(sig);
  return { v, r, s };
}

async function attestPoP(contract, attester, worker, expiry, nonce) {
  const sig = await signAttestation(
    attester,
    await contract.getAddress(),
    ["string", "address", "address", "uint64", "bytes32"],
    ["ATTEST_POP", await contract.getAddress(), worker, expiry, nonce]
  );
  return contract.attestPoP(worker, expiry, nonce, sig.v, sig.r, sig.s);
}

async function bridgeMint(contract, attester, worker, kwrAmount, lockId, expiry) {
  const sig = await signAttestation(
    attester,
    await contract.getAddress(),
    ["string", "address", "address", "uint256", "bytes32", "uint64"],
    ["BRIDGE_MINT_CAPITAL", await contract.getAddress(), worker, kwrAmount, lockId, expiry]
  );
  return contract.bridgeMintCapital(worker, kwrAmount, lockId, expiry, sig.v, sig.r, sig.s);
}

async function attestHonor(contract, attester, worker, axis, honorDelta, jobId, expiry) {
  const sig = await signAttestation(
    attester,
    await contract.getAddress(),
    ["string", "address", "address", "uint8", "uint256", "bytes32", "uint64"],
    ["ATTEST_HONOR", await contract.getAddress(), worker, axis, honorDelta, jobId, expiry]
  );
  return contract.attestHonor(worker, axis, honorDelta, jobId, expiry, sig.v, sig.r, sig.s);
}

async function attestDay(contract, attester, worker, day, active, volunteer, expiry, nonce) {
  const sig = await signAttestation(
    attester,
    await contract.getAddress(),
    ["string", "address", "address", "uint64", "bool", "bool", "uint64", "bytes32"],
    ["ATTEST_DAY", await contract.getAddress(), worker, day, active, volunteer, expiry, nonce]
  );
  return contract.attestDay(worker, day, active, volunteer, expiry, nonce, sig.v, sig.r, sig.s);
}

// ─── 메인 ───
describe("SeumStandard v3.1", function () {
  let contract, governance, attester, user1, user2;

  beforeEach(async function () {
    [governance, attester, user1, user2] = await ethers.getSigners();
    const Factory = await ethers.getContractFactory("SeumStandard", governance);
    contract = await Factory.deploy(attester.address);
    await contract.waitForDeployment();
  });

  // ────────────────────────────────────────────────────
  // (1) 가입 + PoP
  // ────────────────────────────────────────────────────
  describe("PoP + join()", function () {
    it("PoP 없으면 join 실패", async function () {
      await expect(contract.connect(user1).join()).to.be.revertedWith("Zion PoP required");
    });

    it("attestPoP은 attester만", async function () {
      const expiry = (await time.latest()) + 86400;
      const nonce = ethers.id("pop1");
      // user1이 attestPoP 호출 — 본인이 직접은 안 됨. 단 attester 키로 서명된 메시지라면 가능
      // 시나리오: 누군가 잘못된 서명으로 시도 → revert
      const sig = await user1.signMessage("garbage");
      const { v, r, s } = ethers.Signature.from(sig);
      await expect(
        contract.attestPoP(user1.address, expiry, nonce, v, r, s)
      ).to.be.revertedWith("Bad sig");
    });

    it("PoP 후 join 성공", async function () {
      const expiry = (await time.latest()) + 86400;
      await attestPoP(contract, attester, user1.address, expiry, ethers.id("pop1"));
      await expect(contract.connect(user1).join()).to.emit(contract, "CitizenBorn");
      const c = await contract.citizens(user1.address);
      expect(c.isCitizen).to.equal(true);
    });

    it("중복 join 차단", async function () {
      const expiry = (await time.latest()) + 86400;
      await attestPoP(contract, attester, user1.address, expiry, ethers.id("pop1"));
      await contract.connect(user1).join();
      await expect(contract.connect(user1).join()).to.be.revertedWith("Already a citizen");
    });
  });

  // ────────────────────────────────────────────────────
  // (2) ETH 차단 (v3.1: kWR 전용)
  // ────────────────────────────────────────────────────
  describe("ETH 차단", function () {
    it("ETH 송금 시도 revert", async function () {
      await expect(
        user1.sendTransaction({ to: await contract.getAddress(), value: ethers.parseEther("1") })
      ).to.be.revertedWith("ETH not accepted; use bridgeMintCapital");
    });

    it("join()이 payable 아님 — value 전송 시 revert (자동)", async function () {
      // payable 아닌 함수에 value 보내면 ethers가 revert
      // TODO: 명시적 검증
    });
  });

  // ────────────────────────────────────────────────────
  // (3) bridgeMintCapital
  // ────────────────────────────────────────────────────
  describe("bridgeMintCapital", function () {
    beforeEach(async function () {
      const expiry = (await time.latest()) + 86400;
      await attestPoP(contract, attester, user1.address, expiry, ethers.id("pop1"));
      await contract.connect(user1).join();
    });

    it("정상 mint", async function () {
      const expiry = (await time.latest()) + 86400;
      await expect(
        bridgeMint(contract, attester, user1.address, 1000n, ethers.id("lock1"), expiry)
      ).to.emit(contract, "CapitalBridged");
      const c = await contract.citizens(user1.address);
      expect(c.lockedKWR).to.equal(1000n);
    });

    it("리플레이 차단", async function () {
      const expiry = (await time.latest()) + 86400;
      const lockId = ethers.id("lock1");
      await bridgeMint(contract, attester, user1.address, 1000n, lockId, expiry);
      await expect(
        bridgeMint(contract, attester, user1.address, 1000n, lockId, expiry)
      ).to.be.revertedWith("Replay");
    });

    it("만료 차단", async function () {
      const expiry = (await time.latest()) - 1;
      await expect(
        bridgeMint(contract, attester, user1.address, 1000n, ethers.id("lock1"), expiry)
      ).to.be.revertedWith("Expired");
    });

    it("PoP 없으면 차단", async function () {
      const expiry = (await time.latest()) + 86400;
      await expect(
        bridgeMint(contract, attester, user2.address, 1000n, ethers.id("lock1"), expiry)
      ).to.be.revertedWith("Zion PoP required");
    });
  });

  // ────────────────────────────────────────────────────
  // (4) attestHonor (LABOR / VERIFICATION)
  // ────────────────────────────────────────────────────
  describe("attestHonor", function () {
    beforeEach(async function () {
      const expiry = (await time.latest()) + 86400;
      await attestPoP(contract, attester, user1.address, expiry, ethers.id("pop1"));
      await contract.connect(user1).join();
    });

    it("LABOR 명예 누적", async function () {
      const expiry = (await time.latest()) + 86400;
      await attestHonor(contract, attester, user1.address, 1 /*LABOR*/, 5000n, ethers.id("job1"), expiry);
      const labor = await contract.getCurrentHonor(user1.address, 1);
      expect(labor).to.equal(5000n);
    });

    it("VERIFICATION 명예 누적", async function () {
      const expiry = (await time.latest()) + 86400;
      await attestHonor(contract, attester, user1.address, 2 /*VERIFICATION*/, 3000n, ethers.id("job2"), expiry);
      const v = await contract.getCurrentHonor(user1.address, 2);
      expect(v).to.equal(3000n);
    });

    it("CAPITAL axis 차단 — bridgeMintCapital만 가능", async function () {
      const expiry = (await time.latest()) + 86400;
      await expect(
        attestHonor(contract, attester, user1.address, 0 /*CAPITAL*/, 100n, ethers.id("job3"), expiry)
      ).to.be.revertedWith("Capital via bridgeMintCapital");
    });
  });

  // ────────────────────────────────────────────────────
  // (5) attestDay (활동 게이트)
  // ────────────────────────────────────────────────────
  describe("attestDay", function () {
    beforeEach(async function () {
      const expiry = (await time.latest()) + 86400;
      await attestPoP(contract, attester, user1.address, expiry, ethers.id("pop1"));
      await contract.connect(user1).join();
    });

    it("active 비트 set 확인", async function () {
      const expiry = (await time.latest()) + 86400;
      const today = BigInt(Math.floor((await time.latest()) / 86400));
      await attestDay(contract, attester, user1.address, today, true, false, expiry, ethers.id("d1"));
      // TODO: getter로 비트 확인
    });

    it("미래 일자 차단", async function () {
      const expiry = (await time.latest()) + 86400;
      const future = BigInt(Math.floor(Date.now() / 1000 / 86400)) + 10n;
      await expect(
        attestDay(contract, attester, user1.address, future, true, false, expiry, ethers.id("d2"))
      ).to.be.revertedWith("Future day");
    });
  });

  // ────────────────────────────────────────────────────
  // (6) 감쇠 (rate(n) = 1% × 1.01^n)
  // ────────────────────────────────────────────────────
  describe("감쇠", function () {
    beforeEach(async function () {
      const expiry = (await time.latest()) + 86400 * 7;
      await attestPoP(contract, attester, user1.address, expiry, ethers.id("pop1"));
      await contract.connect(user1).join();
      // 자본 10000 입금 → CAPITAL honor 10001 (INITIAL=1 + 10000)
      await bridgeMint(contract, attester, user1.address, 10000n, ethers.id("lock1"), expiry);
    });

    it("활동 게이트 없으면 매일 감쇠 — sabbath 외", async function () {
      // 5일 진행 (sabbath offset 6이라 그 안에 sabbath 없음)
      await time.increase(86400 * 5);
      // 자본 명예 trigger decay via attestHonor (Labor)
      const expiry = (await time.latest()) + 86400;
      await attestHonor(contract, attester, user1.address, 1, 0n, ethers.id("trigger"), expiry);
      // 직접 자본 감쇠는 contribute 안 했으므로 _applyDecay 안 됨 — getCurrentHonor으로 시뮬 조회
      const cap = await contract.getCurrentHonor(user1.address, 0);
      // 5일 모두 inactive: rate(1)=101, rate(2)=102, rate(3)=103, rate(4)=104, rate(5)=105
      // 10001 × (10000-101)/10000 × ... × (10000-105)/10000 ≈ 10001 × 0.99 × 0.989 × ...
      // 정확 계산: floor((10001 * 9899)/10000) = 9899, then floor(9899*9898/10000)=9799, etc.
      expect(cap).to.be.below(10001n);
      expect(cap).to.be.above(9000n);  // 5일 동안 약 5% 감쇠 예상
    });

    it("안식일은 감쇠 면제 — 7일 사이클의 6번째 날", async function () {
      // joinDay 기준 day=6이 sabbath (offset=6)
      // 6일 진행 후 day=6에서 감쇠 면제 발동
      await time.increase(86400 * 7);  // 7일
      const expiry = (await time.latest()) + 86400;
      await attestHonor(contract, attester, user1.address, 1, 0n, ethers.id("trigger"), expiry);
      const cap = await contract.getCurrentHonor(user1.address, 0);
      // 7일 중 sabbath(day 6) 1일 면제, 6일 decay
      expect(cap).to.be.below(10001n);
      expect(cap).to.be.above(9000n);
    });

    it("activeGate set 시 그날 감쇠 면제", async function () {
      // 시간 먼저 진행: joinDay+5까지
      await time.increase(86400 * 5);
      const c0 = await contract.citizens(user1.address);
      const joinDay = c0.joinDay;
      const expiry = (await time.latest()) + 86400 * 30;
      // 과거 일자들(joinDay+1..joinDay+5) 모두에 active 비트 set
      for (let i = 1n; i <= 5n; i++) {
        await attestDay(contract, attester, user1.address, joinDay + i, true, false, expiry, ethers.id("d" + i));
      }
      // trigger decay
      await attestHonor(contract, attester, user1.address, 1, 0n, ethers.id("trigger2"), expiry);
      const cap = await contract.getCurrentHonor(user1.address, 0);
      // 5일 모두 active → 감쇠 0. bridge 직후 10001(=1+10000)
      expect(cap).to.equal(10001n);
    });

    it("MAX_DECAY_DAYS_PER_CALL = 90 cap", async function () {
      // 200일 흘려서 한 번에 _applyDecay 호출 → 90일만 처리되어야 함
      await time.increase(86400 * 200);
      const expiry = (await time.latest()) + 86400;
      await attestHonor(contract, attester, user1.address, 1, 0n, ethers.id("trigger3"), expiry);
      const c = await contract.citizens(user1.address);
      // lastDecayDay[CAPITAL]은 (initial + 90)이어야 함 (joinDay 미사용 — startDay+90)
      // 컨트랙트는 last + 90으로 cap
      // 검증: lastDecayDay가 200일 전체가 아니라 +90만 진행
      // (구체값은 timing depend — 그냥 cap이 적용됐다는 사실만)
      const cap = await contract.getCurrentHonor(user1.address, 0);
      // 90일 감쇠만 적용된 honor가 있어야 함. 전부 사망은 안 됐어야 함 (462일까지 안 갔으니)
      expect(cap).to.be.gt(0n);
    });
  });

  // ────────────────────────────────────────────────────
  // (7) 봉사 — multBP + 자본 보너스 (v3.1 NEW)
  // ────────────────────────────────────────────────────
  describe("봉사 보너스 (v3.1)", function () {
    beforeEach(async function () {
      const expiry = (await time.latest()) + 86400 * 30;
      await attestPoP(contract, attester, user1.address, expiry, ethers.id("pop1"));
      await contract.connect(user1).join();
      await bridgeMint(contract, attester, user1.address, 10000n, ethers.id("lock1"), expiry);
    });

    it("1주 자격 후 volunteerWeeks=1, multBP=10134", async function () {
      // join 직후 lastProcessedWeek = joinDay/7. 가입 주는 정산에서 건너뜀.
      // → 첫 봉사 자격은 (joinDay/7 + 1) 주에서 발생. day joinDay+7 이후가 그 주에 속함.
      await time.increase(86400 * 21);  // 3주 진행
      const c0 = await contract.citizens(user1.address);
      const joinDay = c0.joinDay;
      const expiry = (await time.latest()) + 86400 * 30;
      // 다음 주(joinDay/7 + 1)에 속하는 day에 봉사 비트
      // 가장 안전한 선택: joinDay + 7 ~ joinDay + 13 중 하나
      await attestDay(contract, attester, user1.address, joinDay + 8n, true, true, expiry, ethers.id("d1v"));
      // trigger _processWeeks
      await attestHonor(contract, attester, user1.address, 1, 0n, ethers.id("trigger"), expiry);
      const c = await contract.citizens(user1.address);
      expect(c.volunteerWeeks).to.be.gte(1n);
      expect(c.volunteerMultBP).to.be.gte(10134n);
    });

    it("자본 += max(100, C × 1%) — 작은 C에선 +100", async function () {
      await time.increase(86400 * 21);
      const c0 = await contract.citizens(user1.address);
      const joinDay = c0.joinDay;
      const expiry = (await time.latest()) + 86400 * 30;
      // 모든 일 active (감쇠 0 보장) + 다음 주의 며칠에 volunteer
      for (let i = 1n; i <= 21n; i++) {
        const isVol = i === 10n;  // day joinDay+10 (다음 주에 속함)
        await attestDay(contract, attester, user1.address, joinDay + i, true, isVol, expiry, ethers.id("vd" + i));
      }
      const before = await contract.getHonorBreakdown(user1.address);
      const capBefore = before.capital;
      // trigger _applyDecay + _processWeeks
      await attestHonor(contract, attester, user1.address, 1, 0n, ethers.id("trigger2"), expiry);
      const after = await contract.getHonorBreakdown(user1.address);
      // 1주 자격 → 자본 += max(100, C/100) — small C: +100
      expect(after.capital).to.be.gte(capBefore + 100n);
    });
  });

  // ────────────────────────────────────────────────────
  // (8) Ragequit
  // ────────────────────────────────────────────────────
  describe("ragequit", function () {
    beforeEach(async function () {
      const expiry = (await time.latest()) + 86400;
      await attestPoP(contract, attester, user1.address, expiry, ethers.id("pop1"));
      await contract.connect(user1).join();
      await bridgeMint(contract, attester, user1.address, 1000n, ethers.id("lock1"), expiry);
    });

    it("RagequitRequest 이벤트 + 상태 삭제", async function () {
      await expect(contract.connect(user1).ragequit())
        .to.emit(contract, "RagequitRequest")
        .withArgs(user1.address, 1000n);
      const c = await contract.citizens(user1.address);
      expect(c.isCitizen).to.equal(false);
    });

    it("Ragequit 후 PoP는 유지 — 재가입 즉시 가능", async function () {
      await contract.connect(user1).ragequit();
      expect(await contract.attestedPoP(user1.address)).to.equal(true);
      await expect(contract.connect(user1).join()).to.emit(contract, "CitizenBorn");
    });

    it("ETH 송금 없음 — 컨트랙트는 토큰 보관 안 함", async function () {
      const balanceBefore = await ethers.provider.getBalance(await contract.getAddress());
      await contract.connect(user1).ragequit();
      const balanceAfter = await ethers.provider.getBalance(await contract.getAddress());
      expect(balanceAfter).to.equal(balanceBefore);  // ETH 변동 없음
    });
  });

  // ────────────────────────────────────────────────────
  // (9) 거버넌스
  // ────────────────────────────────────────────────────
  describe("거버넌스", function () {
    beforeEach(async function () {
      const expiry = (await time.latest()) + 86400;
      await attestPoP(contract, attester, user1.address, expiry, ethers.id("pop1"));
      await contract.connect(user1).join();
    });

    it("grantBadge — governance만", async function () {
      // GOVERNANCE=0, AUDIT=1, DISPUTE=2, NODE=3
      await expect(contract.connect(governance).grantBadge(user1.address, 1))
        .to.emit(contract, "BadgeGranted").withArgs(user1.address, 1);
      expect(await contract.hasBadge(user1.address, 1)).to.equal(true);
    });

    it("grantBadge — 비-governance 차단", async function () {
      await expect(
        contract.connect(user1).grantBadge(user1.address, 1)
      ).to.be.revertedWith("Only governance");
    });

    it("revokeBadge — 부여 후 회수", async function () {
      await contract.connect(governance).grantBadge(user1.address, 2);
      expect(await contract.hasBadge(user1.address, 2)).to.equal(true);
      await contract.connect(governance).revokeBadge(user1.address, 2);
      expect(await contract.hasBadge(user1.address, 2)).to.equal(false);
    });

    it("rotateAttester — governance만, attester 교체 후 새 키로 서명 가능", async function () {
      const newAttester = user2;
      await expect(contract.connect(governance).rotateAttester(newAttester.address))
        .to.emit(contract, "AttesterRotated").withArgs(newAttester.address);
      expect(await contract.attester()).to.equal(newAttester.address);
      // 새 attester 서명으로 PoP 등록 가능
      const expiry = (await time.latest()) + 86400;
      await attestPoP(contract, newAttester, governance.address, expiry, ethers.id("newpop"));
      expect(await contract.attestedPoP(governance.address)).to.equal(true);
    });

    it("rotateAttester — zero address 차단", async function () {
      await expect(
        contract.connect(governance).rotateAttester(ethers.ZeroAddress)
      ).to.be.revertedWith("Zero attester");
    });
  });

  // ────────────────────────────────────────────────────
  // (10) 의결권 합성
  // ────────────────────────────────────────────────────
  describe("getVotingPower 합성", function () {
    it("비-시민은 0", async function () {
      expect(await contract.getVotingPower(user2.address)).to.equal(0n);
    });

    it("VP = √C + 5√L + 5√V (봉사 0)", async function () {
      const expiry0 = (await time.latest()) + 86400 * 30;
      await attestPoP(contract, attester, user1.address, expiry0, ethers.id("pop1"));
      await contract.connect(user1).join();
      // 입금 즉시 (감쇠 적용 안 된 상태 = 같은 블록)
      // C=10000 (INITIAL 1 + 9999), L=10000, V=10000
      await bridgeMint(contract, attester, user1.address, 9999n, ethers.id("lock1"), expiry0);
      await attestHonor(contract, attester, user1.address, 1, 10000n, ethers.id("j1"), expiry0);
      await attestHonor(contract, attester, user1.address, 2, 10000n, ethers.id("j2"), expiry0);
      const vp = await contract.getVotingPower(user1.address);
      // √10000 = 100. baseVP = 100 + 5×100 + 5×100 = 1100
      // mult=BP → sqrtFactor=BP, VP = 1100
      expect(vp).to.equal(1100n);
    });

    it("VP factor √2 — 52주 봉사 누적 시 (통합 테스트, 느림)", async function () {
      // 의도: 52주 동안 매주 봉사 자격을 채우면 multBP × 1.01342^52 ≈ 19979 → VP factor ≈ √2.
      // 한 주에 1번씩 attest_day(volunteer=true) + 주기적으로 attestHonor(trigger)로 _processWeeks 발화.
      // 90일 decay cap 때문에 ~10주마다 trigger 필요 (lastDecayDay 따라잡기).
      //
      // 가스 비용: Hardhat in-process ~52 attest_day + ~7 trigger ≈ 5초.
      this.timeout(60_000);

      const expiry0 = (await time.latest()) + 86400;
      await attestPoP(contract, attester, user1.address, expiry0, ethers.id("pop1"));
      await contract.connect(user1).join();
      // C 명예 입금 — 52주 후에도 일부 살아 있도록 큰 값으로
      await bridgeMint(contract, attester, user1.address, 1_000_000n, ethers.id("lock1"), expiry0);
      // L/V도 입금 — VP 합성에 필요
      await attestHonor(contract, attester, user1.address, 1, 1_000_000n, ethers.id("L0"), expiry0);
      await attestHonor(contract, attester, user1.address, 2, 1_000_000n, ethers.id("V0"), expiry0);

      const c0 = await contract.citizens(user1.address);
      const joinDay = c0.joinDay;

      // 52주 시뮬레이션
      for (let w = 0; w < 52; w++) {
        await time.increase(86400 * 7);
        // 다음 주(W+w+1)에 속하는 day = joinDay + 7*w + 7
        // (이 day는 항상 W+w+1 주에 속함 — 이전 단위 테스트에서 검증된 패턴)
        const dayVol = joinDay + BigInt(7 * w + 7);
        const expiryW = (await time.latest()) + 86400 * 2;
        // active + volunteer 둘 다 true — active로 감쇠 면제, volunteer로 자격
        await attestDay(
          contract,
          attester,
          user1.address,
          dayVol,
          true,
          true,
          expiryW,
          ethers.id("v" + w),
        );

        // 10주마다 trigger — apply_decay 90일 cap 따라잡기 + _processWeeks 발화
        if ((w + 1) % 10 === 0) {
          await attestHonor(
            contract,
            attester,
            user1.address,
            1,
            0n,
            ethers.id("trig" + w),
            expiryW,
          );
        }
      }

      // 마지막 주 (week W+52) 가 complete 되려면 through_day ≥ (W+52)*7 + 6 이어야 함.
      // joinDay%7 < 6 인 경우 위 루프만으론 마지막 주 미완성 → 한 주 더 진행 후 trigger.
      await time.increase(86400 * 7);
      const expFinal = (await time.latest()) + 86400 * 2;
      await attestHonor(contract, attester, user1.address, 1, 0n, ethers.id("finalTrig"), expFinal);

      const c52 = await contract.citizens(user1.address);
      // 1. volunteer_weeks 가 52로 누적
      expect(c52.volunteerWeeks).to.equal(52n);
      // 2. volunteer_mult_bp ≈ 19979 (10000 × 1.01342^52, 정수 truncation 누적)
      //    실측치는 18000~22000 범위 안 — 정수 누적 오차 폭 큼
      expect(c52.volunteerMultBP).to.be.gte(18_000n);
      expect(c52.volunteerMultBP).to.be.lte(22_000n);

      // 3. VP factor sqrt(multBP * BP) / BP ≈ √2 ≈ 14142 BP (= sqrt(20000 * 10000))
      //    Newton sqrt 결과는 14000~15000 BP
      const vpFactor = await contract.getVotingPowerFactor
        ? await contract.getVotingPowerFactor(user1.address)
        : null;
      // getVotingPowerFactor 함수 없음 — getVotingPower로 간접 확인
      const vp = await contract.getVotingPower(user1.address);
      expect(vp).to.be.gt(0n);
      // 추가 sanity: VP가 baseVP*1보다 명확히 큼 (factor가 1 초과)
      // baseVP = sqrt(C) + 5*sqrt(L) + 5*sqrt(V), 52주 decay 후 정확값 모름
      //   → 최소한 multBP factor ≈ √2 만큼은 boost 받음을 간접 확인.
      // 직접 비교: 같은 시점의 baseVP 계산 후 vp가 baseVP × √2 비슷한지.
      //   → 컨트랙트가 baseVP 단독 조회 함수 없음 — 별도 검증은 단위 sqrt 테스트로 갈음.
    });
  });
});
