// Copyright 2021 The Smart Chain Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package erc20_test

import (
	"context"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethersphere/sw3-bindings/v3/simpleswapfactory"
	"github.com/yanhuangpai/voyager/pkg/settlement/swap/erc20"
	"github.com/yanhuangpai/voyager/pkg/settlement/swap/transaction"
	backendmock "github.com/yanhuangpai/voyager/pkg/settlement/swap/transaction/backendmock"
	transactionmock "github.com/yanhuangpai/voyager/pkg/settlement/swap/transaction/mock"
)

var (
	erc20ABI = transaction.ParseABIUnchecked(simpleswapfactory.ERC20ABI)
)

func TestBalanceOf(t *testing.T) {
	erc20Address := common.HexToAddress("00")
	account := common.HexToAddress("01")
	expectedBalance := big.NewInt(100)

	erc20 := erc20.New(
		backendmock.New(),
		transactionmock.New(
			transactionmock.WithABICall(
				&erc20ABI,
				expectedBalance.FillBytes(make([]byte, 32)),
				"balanceOf",
				account,
			),
		),
		erc20Address,
	)

	balance, err := erc20.BalanceOf(context.Background(), account)
	if err != nil {
		t.Fatal(err)
	}

	if expectedBalance.Cmp(balance) != 0 {
		t.Fatalf("got wrong balance. wanted %d, got %d", expectedBalance, balance)
	}
}

func TestTransfer(t *testing.T) {
	address := common.HexToAddress("0xabcd")
	account := common.HexToAddress("01")
	value := big.NewInt(20)
	txHash := common.HexToHash("0xdddd")

	erc20 := erc20.New(
		backendmock.New(),
		transactionmock.New(
			transactionmock.WithABISend(&erc20ABI, txHash, address, big.NewInt(0), "transfer", account, value),
		),
		address,
	)

	returnedTxHash, err := erc20.Transfer(context.Background(), account, value)
	if err != nil {
		t.Fatal(err)
	}

	if txHash != returnedTxHash {
		t.Fatalf("returned wrong transaction hash. wanted %v, got %v", txHash, returnedTxHash)
	}
}
