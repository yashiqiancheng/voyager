// Copyright 2020 The Smart Chain Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package netstore provides an abstraction layer over the
// Smart Chain local storage layer that leverages connectivity
// with other peers in order to retrieve chunks from the network that cannot
// be found locally.
package netstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/yanhuangpai/voyager/pkg/infinity"
	"github.com/yanhuangpai/voyager/pkg/logging"
	"github.com/yanhuangpai/voyager/pkg/recovery"
	"github.com/yanhuangpai/voyager/pkg/retrieval"
	"github.com/yanhuangpai/voyager/pkg/sctx"
	"github.com/yanhuangpai/voyager/pkg/storage"
)

type store struct {
	storage.Storer
	retrieval        retrieval.Interface
	logger           logging.Logger
	recoveryCallback recovery.Callback // this is the callback to be executed when a chunk fails to be retrieved
}

var (
	ErrRecoveryAttempt = errors.New("failed to retrieve chunk, recovery initiated")
)

// New returns a new NetStore that wraps a given Storer.
func New(s storage.Storer, rcb recovery.Callback, r retrieval.Interface, logger logging.Logger) storage.Storer {
	return &store{Storer: s, recoveryCallback: rcb, retrieval: r, logger: logger}
}

// Get retrieves a given chunk address.
// It will request a chunk from the network whenever it cannot be found locally.
func (s *store) Get(ctx context.Context, mode storage.ModeGet, addr infinity.Address) (ch infinity.Chunk, err error) {
	ch, err = s.Storer.Get(ctx, mode, addr)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			// request from network
			ch, err = s.retrieval.RetrieveChunk(ctx, addr)
			if err != nil {
				targets := sctx.GetTargets(ctx)
				if targets == nil || s.recoveryCallback == nil {
					return nil, err
				}
				go s.recoveryCallback(addr, targets)
				return nil, ErrRecoveryAttempt
			}

			_, err = s.Storer.Put(ctx, storage.ModePutRequest, ch)
			if err != nil {
				return nil, fmt.Errorf("netstore retrieve put: %w", err)
			}
			return ch, nil
		}
		return nil, fmt.Errorf("netstore get: %w", err)
	}
	return ch, nil
}
