// Copyright 2020 The Smart Chain Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package chequebook_test

import (
	"context"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/yanhuangpai/voyager/pkg/settlement/swap/chequebook"
)

type simpleSwapBindingMock struct {
	balance      func(*bind.CallOpts) (*big.Int, error)
	issuer       func(*bind.CallOpts) (common.Address, error)
	totalPaidOut func(o *bind.CallOpts) (*big.Int, error)
	paidOut      func(*bind.CallOpts, common.Address) (*big.Int, error)
}

func (m *simpleSwapBindingMock) Balance(o *bind.CallOpts) (*big.Int, error) {
	return m.balance(o)
}

func (m *simpleSwapBindingMock) Issuer(o *bind.CallOpts) (common.Address, error) {
	return m.issuer(o)
}

func (m *simpleSwapBindingMock) TotalPaidOut(o *bind.CallOpts) (*big.Int, error) {
	return m.totalPaidOut(o)
}

func (m *simpleSwapBindingMock) PaidOut(o *bind.CallOpts, c common.Address) (*big.Int, error) {
	return m.paidOut(o, c)
}

type chequeSignerMock struct {
	sign func(cheque *chequebook.Cheque) ([]byte, error)
}

func (m *chequeSignerMock) Sign(cheque *chequebook.Cheque) ([]byte, error) {
	return m.sign(cheque)
}

type factoryMock struct {
	erc20Address     func(ctx context.Context) (common.Address, error)
	deploy           func(ctx context.Context, issuer common.Address, defaultHardDepositTimeoutDuration *big.Int) (common.Hash, error)
	waitDeployed     func(ctx context.Context, txHash common.Hash) (common.Address, error)
	verifyBytecode   func(ctx context.Context) error
	verifyChequebook func(ctx context.Context, chequebook common.Address) error
}

// ERC20Address returns the token for which this factory deploys chequebooks.
func (m *factoryMock) ERC20Address(ctx context.Context) (common.Address, error) {
	return m.erc20Address(ctx)
}

func (m *factoryMock) Deploy(ctx context.Context, issuer common.Address, defaultHardDepositTimeoutDuration *big.Int) (common.Hash, error) {
	return m.deploy(ctx, issuer, defaultHardDepositTimeoutDuration)
}

func (m *factoryMock) WaitDeployed(ctx context.Context, txHash common.Hash) (common.Address, error) {
	return m.waitDeployed(ctx, txHash)
}

// VerifyBytecode checks that the factory is valid.
func (m *factoryMock) VerifyBytecode(ctx context.Context) error {
	return m.verifyBytecode(ctx)
}

// VerifyChequebook checks that the supplied chequebook has voyagern deployed by this factory.
func (m *factoryMock) VerifyChequebook(ctx context.Context, chequebook common.Address) error {
	return m.verifyChequebook(ctx, chequebook)
}
