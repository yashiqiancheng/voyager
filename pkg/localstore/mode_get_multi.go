// Copyright 2019 The Smart Chain Authors
// This file is part of the Smart Chain library.
//
// The Smart Chain library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The Smart Chain library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the Smart Chain library. If not, see <http://www.gnu.org/licenses/>.

package localstore

import (
	"context"
	"errors"
	"time"

	"github.com/syndtr/goleveldb/leveldb"
	"github.com/yanhuangpai/voyager/pkg/infinity"
	"github.com/yanhuangpai/voyager/pkg/shed"
	"github.com/yanhuangpai/voyager/pkg/storage"
)

// GetMulti returns chunks from the database. If one of the chunks is not found
// storage.ErrNotFound will be returned. All required indexes will be updated
// required by the Getter Mode. GetMulti is required to implement chunk.Store
// interface.
func (db *DB) GetMulti(ctx context.Context, mode storage.ModeGet, addrs ...infinity.Address) (chunks []infinity.Chunk, err error) {
	db.metrics.ModeGetMulti.Inc()
	defer totalTimeMetric(db.metrics.TotalTimeGetMulti, time.Now())

	defer func() {
		if err != nil {
			db.metrics.ModeGetMultiFailure.Inc()
		}
	}()

	out, err := db.getMulti(mode, addrs...)
	if err != nil {
		if errors.Is(err, leveldb.ErrNotFound) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	chunks = make([]infinity.Chunk, len(out))
	for i, ch := range out {
		chunks[i] = infinity.NewChunk(infinity.NewAddress(ch.Address), ch.Data).WithPinCounter(ch.PinCounter)
	}
	return chunks, nil
}

// getMulti returns Items from the retrieval index
// and updates other indexes.
func (db *DB) getMulti(mode storage.ModeGet, addrs ...infinity.Address) (out []shed.Item, err error) {
	out = make([]shed.Item, len(addrs))
	for i, addr := range addrs {
		out[i].Address = addr.Bytes()
	}

	err = db.retrievalDataIndex.Fill(out)
	if err != nil {
		return nil, err
	}

	switch mode {
	// update the access timestamp and gc index
	case storage.ModeGetRequest:
		db.updateGCItems(out...)

	case storage.ModeGetPin:
		err := db.pinIndex.Fill(out)
		if err != nil {
			return nil, err
		}

	// no updates to indexes
	case storage.ModeGetSync:
	case storage.ModeGetLookup:
	default:
		return out, ErrInvalidMode
	}
	return out, nil
}
