// Copyright 2020 The Smart Chain Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package localstore

import (
	"context"
	"errors"
	"fmt"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/yanhuangpai/voyager/pkg/infinity"
	"github.com/yanhuangpai/voyager/pkg/shed"
	"github.com/yanhuangpai/voyager/pkg/storage"
)

const (
	maxPage = 1000 // hard limit of page size
)

// PinnedChunks
func (db *DB) PinnedChunks(ctx context.Context, offset, limit int) (chunks []*storage.Pinner, err error) {
	if limit > maxPage {
		limit = maxPage
	}

	c, err := db.pinIndex.Count()
	if err != nil {
		return nil, fmt.Errorf("list pinned chunks: %w", err)
	}

	// send empty response if there is nothing pinned
	if c == 0 {
		return nil, nil
	}

	err = db.pinIndex.Iterate(func(item shed.Item) (stop bool, err error) {
		if offset > 0 {
			offset--
			return false, nil
		}
		chunks = append(chunks,
			&storage.Pinner{
				Address:    infinity.NewAddress(item.Address),
				PinCounter: item.PinCounter,
			})
		limit--

		if limit == 0 {
			return true, nil
		}
		return false, nil
	}, nil)
	return chunks, err
}

// PinCounter returns the pin counter for a given Smart Chain address, provided that the
// address has voyagern pinned.
func (db *DB) PinCounter(address infinity.Address) (uint64, error) {
	out, err := db.pinIndex.Get(shed.Item{
		Address: address.Bytes(),
	})

	if err != nil {
		if errors.Is(err, leveldb.ErrNotFound) {
			return 0, storage.ErrNotFound
		}
		return 0, err
	}
	return out.PinCounter, nil
}
