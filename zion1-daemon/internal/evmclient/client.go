// Package evmclient는 SeumStandard 컨트랙트와의 EVM 상호작용.
//
// 두 가지 방향:
//
//	(1) Broadcast* — Zion 이벤트 → EVM 컨트랙트 함수 호출 (attestPoP, bridgeMintCapital, attestHonor, attestDay)
//	(2) WatchRagequit — EVM RagequitRequest 이벤트 감지 → Zion x/seum unlock 핸들러로 전달 (역방향)
package evmclient

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

// SeumStandard ABI — 필요한 함수와 이벤트만 발췌 (전체 ABI 대신 minimal).
// abigen 대신 수동 embed: 데몬이 호출하는 함수가 한정적이고, contract API 변경 시 수정 표면 작음.
const seumABI = `[
  {"type":"function","name":"attestPoP","stateMutability":"nonpayable",
   "inputs":[
     {"name":"worker","type":"address"},
     {"name":"expiry","type":"uint64"},
     {"name":"nonce","type":"bytes32"},
     {"name":"v","type":"uint8"},
     {"name":"r","type":"bytes32"},
     {"name":"s","type":"bytes32"}
   ],"outputs":[]},
  {"type":"function","name":"bridgeMintCapital","stateMutability":"nonpayable",
   "inputs":[
     {"name":"worker","type":"address"},
     {"name":"kWRAmount","type":"uint256"},
     {"name":"lockId","type":"bytes32"},
     {"name":"expiry","type":"uint64"},
     {"name":"v","type":"uint8"},
     {"name":"r","type":"bytes32"},
     {"name":"s","type":"bytes32"}
   ],"outputs":[]},
  {"type":"function","name":"attestHonor","stateMutability":"nonpayable",
   "inputs":[
     {"name":"worker","type":"address"},
     {"name":"axis","type":"uint8"},
     {"name":"honorDelta","type":"uint256"},
     {"name":"jobId","type":"bytes32"},
     {"name":"expiry","type":"uint64"},
     {"name":"v","type":"uint8"},
     {"name":"r","type":"bytes32"},
     {"name":"s","type":"bytes32"}
   ],"outputs":[]},
  {"type":"function","name":"attestDay","stateMutability":"nonpayable",
   "inputs":[
     {"name":"worker","type":"address"},
     {"name":"day","type":"uint64"},
     {"name":"activeGateMet","type":"bool"},
     {"name":"volunteerGateMet","type":"bool"},
     {"name":"expiry","type":"uint64"},
     {"name":"nonce","type":"bytes32"},
     {"name":"v","type":"uint8"},
     {"name":"r","type":"bytes32"},
     {"name":"s","type":"bytes32"}
   ],"outputs":[]},
  {"type":"event","name":"RagequitRequest","anonymous":false,
   "inputs":[
     {"indexed":true,"name":"citizen","type":"address"},
     {"indexed":false,"name":"kWRAmount","type":"uint256"}
   ]}
]`

// Client는 EVM 노드 + SeumStandard 컨트랙트 호출 래퍼.
//
// 동시성: sendTx는 sendMu로 직렬화 — pipeline (Zion 이벤트) 와 ragequit watcher 양쪽이
// 같은 Client를 공유하므로, 병렬 호출 시 동일 PendingNonceAt 값을 얻어 두 번째 tx가
// "nonce too low" 로 reject되는 race 방지.
type Client struct {
	rpc        *ethclient.Client
	chainID    *big.Int
	contract   common.Address
	gasLimit   uint64
	gasPrice   *big.Int
	senderKey  *ecdsa.PrivateKey
	senderAddr common.Address
	parsedABI  abi.ABI

	sendMu sync.Mutex // sendTx 직렬화 (nonce race 방지)
}

// New는 RPC URL과 송신자 키로 클라이언트 생성.
// senderKeyHex는 tx의 from + gas 페이용 키. attester 서명 키와 동일해도 되고 분리해도 됨.
func New(rpcURL string, chainID int64, contractAddr string, gasLimit uint64, gasPriceGwei int64, senderKeyHex string) (*Client, error) {
	rpc, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("ethclient.Dial: %w", err)
	}
	addr := common.HexToAddress(contractAddr)
	if (addr == common.Address{}) {
		return nil, fmt.Errorf("contract address is zero")
	}

	// sender key parsing
	senderKeyHex = strings.TrimPrefix(senderKeyHex, "0x")
	if len(senderKeyHex) != 64 {
		return nil, fmt.Errorf("sender key must be 64 hex chars, got %d", len(senderKeyHex))
	}
	keyBytes, err := hex.DecodeString(senderKeyHex)
	if err != nil {
		return nil, fmt.Errorf("decode sender hex: %w", err)
	}
	senderKey, err := crypto.ToECDSA(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("parse sender ECDSA: %w", err)
	}
	pub, ok := senderKey.Public().(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("sender public key not ECDSA")
	}
	senderAddr := crypto.PubkeyToAddress(*pub)

	parsed, err := abi.JSON(strings.NewReader(seumABI))
	if err != nil {
		return nil, fmt.Errorf("parse SeumStandard ABI: %w", err)
	}

	gasPrice := new(big.Int).Mul(big.NewInt(gasPriceGwei), big.NewInt(1_000_000_000))
	return &Client{
		rpc:        rpc,
		chainID:    big.NewInt(chainID),
		contract:   addr,
		gasLimit:   gasLimit,
		gasPrice:   gasPrice,
		senderKey:  senderKey,
		senderAddr: senderAddr,
		parsedABI:  parsed,
	}, nil
}

// Close는 RPC 연결 종료.
func (c *Client) Close() {
	c.rpc.Close()
}

// Address는 컨트랙트 주소.
func (c *Client) Address() common.Address { return c.contract }

// ChainID는 EVM chain ID (서명 정합성용).
func (c *Client) ChainID() *big.Int { return c.chainID }

// SenderAddress는 tx 송신자 EVM 주소 (gas 잔액 모니터링용).
func (c *Client) SenderAddress() common.Address { return c.senderAddr }

// ─── Broadcast* — 컨트랙트 함수 호출 ───

// BroadcastAttestPoP는 SeumStandard.attestPoP 호출.
func (c *Client) BroadcastAttestPoP(ctx context.Context, worker common.Address, expiry uint64, nonce [32]byte, v uint8, r, s [32]byte) (*types.Transaction, error) {
	return c.sendTx(ctx, "attestPoP", worker, expiry, nonce, v, r, s)
}

// BroadcastBridgeMintCapital는 SeumStandard.bridgeMintCapital 호출.
func (c *Client) BroadcastBridgeMintCapital(ctx context.Context, worker common.Address, kWRAmount *big.Int, lockID [32]byte, expiry uint64, v uint8, r, s [32]byte) (*types.Transaction, error) {
	return c.sendTx(ctx, "bridgeMintCapital", worker, kWRAmount, lockID, expiry, v, r, s)
}

// BroadcastAttestHonor는 SeumStandard.attestHonor 호출.
func (c *Client) BroadcastAttestHonor(ctx context.Context, worker common.Address, axis uint8, honorDelta *big.Int, jobID [32]byte, expiry uint64, v uint8, r, s [32]byte) (*types.Transaction, error) {
	return c.sendTx(ctx, "attestHonor", worker, axis, honorDelta, jobID, expiry, v, r, s)
}

// BroadcastAttestDay는 SeumStandard.attestDay 호출.
func (c *Client) BroadcastAttestDay(ctx context.Context, worker common.Address, day uint64, activeGate, volunteerGate bool, expiry uint64, nonce [32]byte, v uint8, r, s [32]byte) (*types.Transaction, error) {
	return c.sendTx(ctx, "attestDay", worker, day, activeGate, volunteerGate, expiry, nonce, v, r, s)
}

// sendTx는 ABI 패킹 + nonce 조회 + 서명 + 송출의 공통 흐름.
// sendMu로 직렬화: 동시 호출 시 PendingNonceAt 값이 갱신될 시간 보장.
func (c *Client) sendTx(ctx context.Context, method string, args ...interface{}) (*types.Transaction, error) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	calldata, err := c.parsedABI.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("abi pack %s: %w", method, err)
	}

	nonce, err := c.rpc.PendingNonceAt(ctx, c.senderAddr)
	if err != nil {
		return nil, fmt.Errorf("pending nonce: %w", err)
	}

	tx := types.NewTransaction(nonce, c.contract, big.NewInt(0), c.gasLimit, c.gasPrice, calldata)
	signedTx, err := types.SignTx(tx, types.NewEIP155Signer(c.chainID), c.senderKey)
	if err != nil {
		return nil, fmt.Errorf("sign tx %s: %w", method, err)
	}

	if err := c.rpc.SendTransaction(ctx, signedTx); err != nil {
		return nil, fmt.Errorf("send tx %s: %w", method, err)
	}
	return signedTx, nil
}

// ─── WatchRagequit — 역방향 (EVM → Zion) ───

// RagequitEvent는 EVM RagequitRequest 디코딩 결과.
type RagequitEvent struct {
	Citizen     common.Address
	KWRAmount   *big.Int
	BlockNumber uint64
	BlockHash   common.Hash
	TxHash      common.Hash
	LogIndex    uint
}

// WatchRagequit는 SeumStandard.RagequitRequest 이벤트 구독.
// 결과 채널이 닫히면 (혹은 ctx cancel) 종료.
// 구독 실패 시 error 반환 — caller가 backoff + 재구독 책임.
func (c *Client) WatchRagequit(ctx context.Context, fromBlock *big.Int, out chan<- RagequitEvent) error {
	eventABI, ok := c.parsedABI.Events["RagequitRequest"]
	if !ok {
		return fmt.Errorf("RagequitRequest event not in ABI")
	}
	topic0 := eventABI.ID

	query := ethereum.FilterQuery{
		FromBlock: fromBlock,
		Addresses: []common.Address{c.contract},
		Topics:    [][]common.Hash{{topic0}},
	}

	logs := make(chan types.Log, 16)
	sub, err := c.rpc.SubscribeFilterLogs(ctx, query, logs)
	if err != nil {
		return fmt.Errorf("subscribe filter logs: %w", err)
	}
	defer sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-sub.Err():
			return fmt.Errorf("subscription error: %w", err)
		case log := <-logs:
			ev, decErr := c.decodeRagequit(log)
			if decErr != nil {
				// log 한 줄 깨졌다고 전체 종료하지 않음 — caller가 logger로 alarm
				continue
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
}

// decodeRagequit는 raw log → RagequitEvent. citizen은 indexed (topic[1]), amount는 data.
func (c *Client) decodeRagequit(log types.Log) (RagequitEvent, error) {
	if len(log.Topics) < 2 {
		return RagequitEvent{}, fmt.Errorf("expected 2 topics, got %d", len(log.Topics))
	}
	citizen := common.BytesToAddress(log.Topics[1].Bytes())

	eventABI := c.parsedABI.Events["RagequitRequest"]
	values, err := eventABI.Inputs.NonIndexed().UnpackValues(log.Data)
	if err != nil {
		return RagequitEvent{}, fmt.Errorf("unpack data: %w", err)
	}
	if len(values) != 1 {
		return RagequitEvent{}, fmt.Errorf("expected 1 non-indexed value, got %d", len(values))
	}
	amount, ok := values[0].(*big.Int)
	if !ok {
		return RagequitEvent{}, fmt.Errorf("amount not *big.Int: %T", values[0])
	}

	return RagequitEvent{
		Citizen:     citizen,
		KWRAmount:   amount,
		BlockNumber: log.BlockNumber,
		BlockHash:   log.BlockHash,
		TxHash:      log.TxHash,
		LogIndex:    log.Index,
	}, nil
}
