// Package evmclient는 SeumStandard 컨트랙트와의 EVM 상호작용.
package evmclient

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
)

// Client는 EVM 노드 + SeumStandard 컨트랙트 호출 래퍼.
type Client struct {
	rpc        *ethclient.Client
	chainID    *big.Int
	contract   common.Address
	gasLimit   uint64
	gasPrice   *big.Int
	senderKey  *ecdsa.PrivateKey   // tx 송신용 — attester 키와 다를 수 있음
	senderAddr common.Address
}

// New는 RPC URL과 송신자 키로 클라이언트 생성.
// senderKey는 tx의 from + gas 페이용 키. attester 서명 키와 동일해도 되고 분리해도 됨.
func New(rpcURL string, chainID int64, contractAddr string, gasLimit uint64, gasPriceGwei int64, senderKeyHex string) (*Client, error) {
	rpc, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("ethclient.Dial: %w", err)
	}
	addr := common.HexToAddress(contractAddr)
	if (addr == common.Address{}) {
		return nil, fmt.Errorf("contract address is zero")
	}
	// TODO: senderKey parsing — signer 패키지의 헬퍼 재사용
	gasPrice := new(big.Int).Mul(big.NewInt(gasPriceGwei), big.NewInt(1e9))
	return &Client{
		rpc:       rpc,
		chainID:   big.NewInt(chainID),
		contract:  addr,
		gasLimit:  gasLimit,
		gasPrice:  gasPrice,
		senderKey: nil, // TODO: parse senderKeyHex
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

// BroadcastAttestPoP는 SeumStandard.attestPoP 호출.
// TODO: 실제 ABI bind 사용. 아래는 인터페이스 스케치.
func (c *Client) BroadcastAttestPoP(ctx context.Context, worker common.Address, expiry uint64, nonce [32]byte, v uint8, r, s [32]byte) (*types.Transaction, error) {
	// 단계:
	// 1. nonce 조회 (PendingNonceAt)
	// 2. abi.Pack("attestPoP", worker, expiry, nonce, v, r, s) → calldata
	// 3. types.NewTransaction(...)
	// 4. types.SignTx(tx, types.NewEIP155Signer(c.chainID), c.senderKey)
	// 5. c.rpc.SendTransaction(ctx, signedTx)
	return nil, fmt.Errorf("TODO: implement ABI binding for attestPoP")
}

// BroadcastBridgeMintCapital는 SeumStandard.bridgeMintCapital 호출.
func (c *Client) BroadcastBridgeMintCapital(ctx context.Context, worker common.Address, kWRAmount *big.Int, lockId [32]byte, expiry uint64, v uint8, r, s [32]byte) (*types.Transaction, error) {
	return nil, fmt.Errorf("TODO: implement ABI binding for bridgeMintCapital")
}

// BroadcastAttestHonor는 SeumStandard.attestHonor 호출.
func (c *Client) BroadcastAttestHonor(ctx context.Context, worker common.Address, axis uint8, honorDelta *big.Int, jobId [32]byte, expiry uint64, v uint8, r, s [32]byte) (*types.Transaction, error) {
	return nil, fmt.Errorf("TODO: implement ABI binding for attestHonor")
}

// BroadcastAttestDay는 SeumStandard.attestDay 호출.
func (c *Client) BroadcastAttestDay(ctx context.Context, worker common.Address, day uint64, activeGate, volunteerGate bool, expiry uint64, nonce [32]byte, v uint8, r, s [32]byte) (*types.Transaction, error) {
	return nil, fmt.Errorf("TODO: implement ABI binding for attestDay")
}

// WatchRagequit는 SeumStandard.RagequitRequest 이벤트 구독.
// EVM ragequit이 일어나면 Zion에 unlock tx 송출 필요.
func (c *Client) WatchRagequit(ctx context.Context, ch chan<- RagequitEvent) error {
	// TODO: ethclient.SubscribeFilterLogs(...)
	return fmt.Errorf("TODO: implement EVM event subscribe")
}

// RagequitEvent는 EVM RagequitRequest 디코딩 결과.
type RagequitEvent struct {
	Citizen   common.Address
	KWRAmount *big.Int
	BlockHash common.Hash
	TxHash    common.Hash
}

// makeTransactor는 헬퍼: signed tx 옵션.
func (c *Client) makeTransactor(ctx context.Context) (*bind.TransactOpts, error) {
	if c.senderKey == nil {
		return nil, fmt.Errorf("sender key not configured")
	}
	opts, err := bind.NewKeyedTransactorWithChainID(c.senderKey, c.chainID)
	if err != nil {
		return nil, err
	}
	opts.GasLimit = c.gasLimit
	opts.GasPrice = c.gasPrice
	opts.Context = ctx
	return opts, nil
}
