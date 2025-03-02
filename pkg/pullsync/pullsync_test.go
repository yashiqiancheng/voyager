// Copyright 2020 The Smart Chain Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pullsync_test

import (
	"context"
	"errors"
	"io"
	"io/ioutil"
	"testing"

	"github.com/yanhuangpai/voyager/pkg/infinity"
	"github.com/yanhuangpai/voyager/pkg/logging"
	"github.com/yanhuangpai/voyager/pkg/p2p"
	"github.com/yanhuangpai/voyager/pkg/p2p/streamtest"
	"github.com/yanhuangpai/voyager/pkg/pullsync"
	"github.com/yanhuangpai/voyager/pkg/pullsync/pullstorage/mock"
	testingc "github.com/yanhuangpai/voyager/pkg/storage/testing"
)

var (
	addrs  []infinity.Address
	chunks []infinity.Chunk
)

func someChunks(i ...int) (c []infinity.Chunk) {
	for _, v := range i {
		c = append(c, chunks[v])
	}
	return c
}

func init() {
	n := 5
	chunks = make([]infinity.Chunk, n)
	addrs = make([]infinity.Address, n)
	for i := 0; i < n; i++ {
		chunks[i] = testingc.GenerateTestRandomChunk()
		addrs[i] = chunks[i].Address()
	}
}

// TestIncoming tests that an incoming request for an interval
// is handled correctly when no chunks are available in the interval.
// This means the interval exists but chunks are not there (GCd).
// Expected behavior is that an offer message with the requested
// To value is returned to the requester, but offer.Hashes is empty.
func TestIncoming_WantEmptyInterval(t *testing.T) {
	var (
		mockTopmost        = uint64(5)
		ps, _              = newPullSync(nil, mock.WithIntervalsResp([]infinity.Address{}, mockTopmost, nil))
		recorder           = streamtest.New(streamtest.WithProtocols(ps.Protocol()))
		psClient, clientDb = newPullSync(recorder)
	)

	topmost, _, err := psClient.SyncInterval(context.Background(), infinity.ZeroAddress, 1, 0, 5)
	if err != nil {
		t.Fatal(err)
	}

	if topmost != mockTopmost {
		t.Fatalf("got offer topmost %d but want %d", topmost, mockTopmost)
	}

	if clientDb.PutCalls() > 0 {
		t.Fatal("too many puts")
	}

}
func TestIncoming_WantNone(t *testing.T) {
	var (
		mockTopmost        = uint64(5)
		ps, _              = newPullSync(nil, mock.WithIntervalsResp(addrs, mockTopmost, nil), mock.WithChunks(chunks...))
		recorder           = streamtest.New(streamtest.WithProtocols(ps.Protocol()))
		psClient, clientDb = newPullSync(recorder, mock.WithChunks(chunks...))
	)

	topmost, _, err := psClient.SyncInterval(context.Background(), infinity.ZeroAddress, 0, 0, 5)
	if err != nil {
		t.Fatal(err)
	}

	if topmost != mockTopmost {
		t.Fatalf("got offer topmost %d but want %d", topmost, mockTopmost)
	}
	if clientDb.PutCalls() > 0 {
		t.Fatal("too many puts")
	}
}

func TestIncoming_WantOne(t *testing.T) {
	var (
		mockTopmost        = uint64(5)
		ps, _              = newPullSync(nil, mock.WithIntervalsResp(addrs, mockTopmost, nil), mock.WithChunks(chunks...))
		recorder           = streamtest.New(streamtest.WithProtocols(ps.Protocol()))
		psClient, clientDb = newPullSync(recorder, mock.WithChunks(someChunks(1, 2, 3, 4)...))
	)

	topmost, _, err := psClient.SyncInterval(context.Background(), infinity.ZeroAddress, 0, 0, 5)
	if err != nil {
		t.Fatal(err)
	}

	if topmost != mockTopmost {
		t.Fatalf("got offer topmost %d but want %d", topmost, mockTopmost)
	}

	// should have all
	haveChunks(t, clientDb, addrs...)
	if clientDb.PutCalls() > 1 {
		t.Fatal("too many puts")
	}
}

func TestIncoming_WantAll(t *testing.T) {
	var (
		mockTopmost        = uint64(5)
		ps, _              = newPullSync(nil, mock.WithIntervalsResp(addrs, mockTopmost, nil), mock.WithChunks(chunks...))
		recorder           = streamtest.New(streamtest.WithProtocols(ps.Protocol()))
		psClient, clientDb = newPullSync(recorder)
	)

	topmost, _, err := psClient.SyncInterval(context.Background(), infinity.ZeroAddress, 0, 0, 5)
	if err != nil {
		t.Fatal(err)
	}

	if topmost != mockTopmost {
		t.Fatalf("got offer topmost %d but want %d", topmost, mockTopmost)
	}

	// should have all
	haveChunks(t, clientDb, addrs...)
	if p := clientDb.PutCalls(); p != 1 {
		t.Fatalf("want %d puts but got %d", 1, p)
	}
}

func TestIncoming_UnsolicitedChunk(t *testing.T) {
	evilAddr := infinity.MustParseHexAddress("0000000000000000000000000000000000000000000000000000000000000666")
	evilData := []byte{0x66, 0x66, 0x66}
	evil := infinity.NewChunk(evilAddr, evilData)

	var (
		mockTopmost = uint64(5)
		ps, _       = newPullSync(nil, mock.WithIntervalsResp(addrs, mockTopmost, nil), mock.WithChunks(chunks...), mock.WithEvilChunk(addrs[4], evil))
		recorder    = streamtest.New(streamtest.WithProtocols(ps.Protocol()))
		psClient, _ = newPullSync(recorder)
	)

	_, _, err := psClient.SyncInterval(context.Background(), infinity.ZeroAddress, 0, 0, 5)
	if !errors.Is(err, pullsync.ErrUnsolicitedChunk) {
		t.Fatalf("expected ErrUnsolicitedChunk but got %v", err)
	}
}

func TestGetCursors(t *testing.T) {
	var (
		mockCursors = []uint64{100, 101, 102, 103}
		ps, _       = newPullSync(nil, mock.WithCursors(mockCursors))
		recorder    = streamtest.New(streamtest.WithProtocols(ps.Protocol()))
		psClient, _ = newPullSync(recorder)
	)

	curs, err := psClient.GetCursors(context.Background(), infinity.ZeroAddress)
	if err != nil {
		t.Fatal(err)
	}

	if len(curs) != len(mockCursors) {
		t.Fatalf("length mismatch got %d want %d", len(curs), len(mockCursors))
	}

	for i, v := range mockCursors {
		if curs[i] != v {
			t.Errorf("cursor mismatch. index %d want %d got %d", i, v, curs[i])
		}
	}
}

func TestGetCursorsError(t *testing.T) {
	var (
		e           = errors.New("erring")
		ps, _       = newPullSync(nil, mock.WithCursorsErr(e))
		recorder    = streamtest.New(streamtest.WithProtocols(ps.Protocol()))
		psClient, _ = newPullSync(recorder)
	)

	_, err := psClient.GetCursors(context.Background(), infinity.ZeroAddress)
	if err == nil {
		t.Fatal("expected error but got none")
	}
	if !errors.Is(err, io.EOF) {
		t.Fatalf("expect error '%v' but got '%v'", e, err)
	}
}

func haveChunks(t *testing.T, s *mock.PullStorage, addrs ...infinity.Address) {
	t.Helper()
	for _, a := range addrs {
		have, err := s.Has(context.Background(), a)
		if err != nil {
			t.Fatal(err)
		}
		if !have {
			t.Errorf("storage does not have chunk %s", a)
		}
	}
}

func newPullSync(s p2p.Streamer, o ...mock.Option) (*pullsync.Syncer, *mock.PullStorage) {
	storage := mock.NewPullStorage(o...)
	logger := logging.New(ioutil.Discard, 0)
	unwrap := func(infinity.Chunk) {}
	return pullsync.New(s, storage, unwrap, logger), storage
}
