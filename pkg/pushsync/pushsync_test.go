// Copyright 2020 The Smart Chain Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pushsync_test

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"testing"
	"time"

	"github.com/yanhuangpai/voyager/pkg/accounting"
	accountingmock "github.com/yanhuangpai/voyager/pkg/accounting/mock"
	"github.com/yanhuangpai/voyager/pkg/infinity"
	"github.com/yanhuangpai/voyager/pkg/localstore"
	"github.com/yanhuangpai/voyager/pkg/logging"
	"github.com/yanhuangpai/voyager/pkg/p2p"
	"github.com/yanhuangpai/voyager/pkg/p2p/protobuf"
	"github.com/yanhuangpai/voyager/pkg/p2p/streamtest"
	"github.com/yanhuangpai/voyager/pkg/pushsync"
	"github.com/yanhuangpai/voyager/pkg/pushsync/pb"
	statestore "github.com/yanhuangpai/voyager/pkg/statestore/mock"
	testingc "github.com/yanhuangpai/voyager/pkg/storage/testing"
	"github.com/yanhuangpai/voyager/pkg/tags"
	"github.com/yanhuangpai/voyager/pkg/topology"
	"github.com/yanhuangpai/voyager/pkg/topology/mock"
)

const (
	fixedPrice = uint64(10)
)

// TestSendChunkAndGetReceipt inserts a chunk as uploaded chunk in db. This triggers sending a chunk to the closest node
// and expects a receipt. The message are intercepted in the outgoing stream to check for correctness.
func TestSendChunkAndReceiveReceipt(t *testing.T) {
	// chunk data to upload
	chunk := testingc.FixtureChunk("7000")

	// create a pivot node and a mocked closest node
	pivotNode := infinity.MustParseHexAddress("0000000000000000000000000000000000000000000000000000000000000000")   // base is 0000
	closestPeer := infinity.MustParseHexAddress("6000000000000000000000000000000000000000000000000000000000000000") // binary 0110 -> po 1

	// peer is the node responding to the chunk receipt message
	// mock should return ErrWantSelf since there's no one to forward to
	psPeer, storerPeer, _, peerAccounting := createPushSyncNode(t, closestPeer, nil, nil, mock.WithClosestPeerErr(topology.ErrWantSelf))
	defer storerPeer.Close()

	recorder := streamtest.New(streamtest.WithProtocols(psPeer.Protocol()), streamtest.WithBaseAddr(pivotNode))

	// pivot node needs the streamer since the chunk is intercepted by
	// the chunk worker, then gets sent by opening a new stream
	psPivot, storerPivot, _, pivotAccounting := createPushSyncNode(t, pivotNode, recorder, nil, mock.WithClosestPeer(closestPeer))
	defer storerPivot.Close()

	// Trigger the sending of chunk to the closest node
	receipt, err := psPivot.PushChunkToClosest(context.Background(), chunk)
	if err != nil {
		t.Fatal(err)
	}

	if !chunk.Address().Equal(receipt.Address) {
		t.Fatal("invalid receipt")
	}

	// this intercepts the outgoing delivery message
	waitOnRecordAndTest(t, closestPeer, recorder, chunk.Address(), chunk.Data())

	// this intercepts the incoming receipt message
	waitOnRecordAndTest(t, closestPeer, recorder, chunk.Address(), nil)
	balance, err := pivotAccounting.Balance(closestPeer)
	if err != nil {
		t.Fatal(err)
	}

	if balance.Int64() != -int64(fixedPrice) {
		t.Fatalf("unexpected balance on pivot. want %d got %d", -int64(fixedPrice), balance)
	}

	balance, err = peerAccounting.Balance(pivotNode)
	if err != nil {
		t.Fatal(err)
	}
	if balance.Int64() != int64(fixedPrice) {
		t.Fatalf("unexpected balance on peer. want %d got %d", int64(fixedPrice), balance)
	}
}

// PushChunkToClosest tests the sending of chunk to closest peer from the origination source perspective.
// it also checks wether the tags are incremented properly if they are present
func TestPushChunkToClosest(t *testing.T) {
	// chunk data to upload
	chunk := testingc.FixtureChunk("7000")
	// create a pivot node and a mocked closest node
	pivotNode := infinity.MustParseHexAddress("0000")   // base is 0000
	closestPeer := infinity.MustParseHexAddress("6000") // binary 0110 -> po 1
	callbackC := make(chan struct{}, 1)
	// peer is the node responding to the chunk receipt message
	// mock should return ErrWantSelf since there's no one to forward to
	psPeer, storerPeer, _, peerAccounting := createPushSyncNode(t, closestPeer, nil, chanFunc(callbackC), mock.WithClosestPeerErr(topology.ErrWantSelf))
	defer storerPeer.Close()

	recorder := streamtest.New(streamtest.WithProtocols(psPeer.Protocol()), streamtest.WithBaseAddr(pivotNode))

	// pivot node needs the streamer since the chunk is intercepted by
	// the chunk worker, then gets sent by opening a new stream
	psPivot, storerPivot, pivotTags, pivotAccounting := createPushSyncNode(t, pivotNode, recorder, nil, mock.WithClosestPeer(closestPeer))
	defer storerPivot.Close()

	ta, err := pivotTags.Create(1)
	if err != nil {
		t.Fatal(err)
	}
	chunk = chunk.WithTagID(ta.Uid)

	ta1, err := pivotTags.Get(ta.Uid)
	if err != nil {
		t.Fatal(err)
	}

	if ta1.Get(tags.StateSent) != 0 || ta1.Get(tags.StateSynced) != 0 {
		t.Fatalf("tags initialization error")
	}

	// Trigger the sending of chunk to the closest node
	receipt, err := psPivot.PushChunkToClosest(context.Background(), chunk)
	if err != nil {
		t.Fatal(err)
	}

	if !chunk.Address().Equal(receipt.Address) {
		t.Fatal("invalid receipt")
	}

	// this intercepts the outgoing delivery message
	waitOnRecordAndTest(t, closestPeer, recorder, chunk.Address(), chunk.Data())

	// this intercepts the incoming receipt message
	waitOnRecordAndTest(t, closestPeer, recorder, chunk.Address(), nil)

	ta2, err := pivotTags.Get(ta.Uid)
	if err != nil {
		t.Fatal(err)
	}
	if ta2.Get(tags.StateSent) != 1 {
		t.Fatalf("tags error")
	}

	balance, err := pivotAccounting.Balance(closestPeer)
	if err != nil {
		t.Fatal(err)
	}

	if balance.Int64() != -int64(fixedPrice) {
		t.Fatalf("unexpected balance on pivot. want %d got %d", -int64(fixedPrice), balance)
	}

	balance, err = peerAccounting.Balance(pivotNode)
	if err != nil {
		t.Fatal(err)
	}

	if balance.Int64() != int64(fixedPrice) {
		t.Fatalf("unexpected balance on peer. want %d got %d", int64(fixedPrice), balance)
	}

	// check if the pss delivery hook is called
	select {
	case <-callbackC:
	case <-time.After(100 * time.Millisecond):
		t.Fatalf("delivery hook was not called")
	}
}

func TestPushChunkToNextClosest(t *testing.T) {
	// chunk data to upload
	chunk := testingc.FixtureChunk("7000")

	// create a pivot node and a mocked closest node
	pivotNode := infinity.MustParseHexAddress("0000") // base is 0000

	peer1 := infinity.MustParseHexAddress("6000")
	peer2 := infinity.MustParseHexAddress("5000")
	peers := []infinity.Address{
		peer1,
		peer2,
	}

	// peer is the node responding to the chunk receipt message
	// mock should return ErrWantSelf since there's no one to forward to
	psPeer1, storerPeer1, _, peerAccounting1 := createPushSyncNode(t, peer1, nil, nil, mock.WithClosestPeerErr(topology.ErrWantSelf))
	defer storerPeer1.Close()

	psPeer2, storerPeer2, _, peerAccounting2 := createPushSyncNode(t, peer2, nil, nil, mock.WithClosestPeerErr(topology.ErrWantSelf))
	defer storerPeer2.Close()

	recorder := streamtest.New(
		streamtest.WithProtocols(
			psPeer1.Protocol(),
			psPeer2.Protocol(),
		),
		streamtest.WithMiddlewares(
			func(h p2p.HandlerFunc) p2p.HandlerFunc {
				return func(ctx context.Context, peer p2p.Peer, stream p2p.Stream) error {
					// NOTE: return error for peer1
					if peer1.Equal(peer.Address) {
						return fmt.Errorf("peer not reachable: %s", peer.Address.String())
					}

					if err := h(ctx, peer, stream); err != nil {
						return err
					}
					// close stream after all previous middlewares wrote to it
					// so that the receiving peer can get all the post messages
					return stream.Close()
				}
			},
		),
		streamtest.WithBaseAddr(pivotNode),
	)

	// pivot node needs the streamer since the chunk is intercepted by
	// the chunk worker, then gets sent by opening a new stream
	psPivot, storerPivot, pivotTags, pivotAccounting := createPushSyncNode(t, pivotNode, recorder, nil,
		mock.WithPeers(peers...),
	)
	defer storerPivot.Close()

	ta, err := pivotTags.Create(1)
	if err != nil {
		t.Fatal(err)
	}
	chunk = chunk.WithTagID(ta.Uid)

	ta1, err := pivotTags.Get(ta.Uid)
	if err != nil {
		t.Fatal(err)
	}

	if ta1.Get(tags.StateSent) != 0 || ta1.Get(tags.StateSynced) != 0 {
		t.Fatalf("tags initialization error")
	}

	// Trigger the sending of chunk to the closest node
	receipt, err := psPivot.PushChunkToClosest(context.Background(), chunk)
	if err != nil {
		t.Fatal(err)
	}

	if !chunk.Address().Equal(receipt.Address) {
		t.Fatal("invalid receipt")
	}

	// this intercepts the outgoing delivery message
	waitOnRecordAndTest(t, peer2, recorder, chunk.Address(), chunk.Data())

	// this intercepts the incoming receipt message
	waitOnRecordAndTest(t, peer2, recorder, chunk.Address(), nil)

	ta2, err := pivotTags.Get(ta.Uid)
	if err != nil {
		t.Fatal(err)
	}
	if ta2.Get(tags.StateSent) != 1 {
		t.Fatalf("tags error")
	}

	balance, err := pivotAccounting.Balance(peer2)
	if err != nil {
		t.Fatal(err)
	}

	if balance.Int64() != -int64(fixedPrice) {
		t.Fatalf("unexpected balance on pivot. want %d got %d", -int64(fixedPrice), balance)
	}

	balance2, err := peerAccounting2.Balance(pivotNode)
	if err != nil {
		t.Fatal(err)
	}

	if balance2.Int64() != int64(fixedPrice) {
		t.Fatalf("unexpected balance on peer2. want %d got %d", int64(fixedPrice), balance2)
	}

	balance1, err := peerAccounting1.Balance(peer1)
	if err != nil {
		t.Fatal(err)
	}

	if balance1.Int64() != 0 {
		t.Fatalf("unexpected balance on peer1. want %d got %d", 0, balance1)
	}
}

// TestHandler expect a chunk from a node on a stream. It then stores the chunk in the local store and
// sends back a receipt. This is tested by intercepting the incoming stream for proper messages.
// It also sends the chunk to the closest peer and receives a receipt.
//
// Chunk moves from   TriggerPeer -> PivotPeer -> ClosestPeer
func TestHandler(t *testing.T) {
	// chunk data to upload
	chunk := testingc.FixtureChunk("7000")

	// create a pivot node and a mocked closest node
	pivotPeer := infinity.MustParseHexAddress("0000000000000000000000000000000000000000000000000000000000000000")
	triggerPeer := infinity.MustParseHexAddress("6000000000000000000000000000000000000000000000000000000000000000")
	closestPeer := infinity.MustParseHexAddress("f000000000000000000000000000000000000000000000000000000000000000")

	// Create the closest peer
	psClosestPeer, closestStorerPeerDB, _, closestAccounting := createPushSyncNode(t, closestPeer, nil, nil, mock.WithClosestPeerErr(topology.ErrWantSelf))
	defer closestStorerPeerDB.Close()

	closestRecorder := streamtest.New(streamtest.WithProtocols(psClosestPeer.Protocol()), streamtest.WithBaseAddr(pivotPeer))

	// creating the pivot peer
	psPivot, storerPivotDB, _, pivotAccounting := createPushSyncNode(t, pivotPeer, closestRecorder, nil, mock.WithClosestPeer(closestPeer))
	defer storerPivotDB.Close()

	pivotRecorder := streamtest.New(streamtest.WithProtocols(psPivot.Protocol()), streamtest.WithBaseAddr(triggerPeer))

	// Creating the trigger peer
	psTriggerPeer, triggerStorerDB, _, triggerAccounting := createPushSyncNode(t, triggerPeer, pivotRecorder, nil, mock.WithClosestPeer(pivotPeer))
	defer triggerStorerDB.Close()

	receipt, err := psTriggerPeer.PushChunkToClosest(context.Background(), chunk)
	if err != nil {
		t.Fatal(err)
	}

	if !chunk.Address().Equal(receipt.Address) {
		t.Fatal("invalid receipt")
	}

	// In pivot peer,  intercept the incoming delivery chunk from the trigger peer and check for correctness
	waitOnRecordAndTest(t, pivotPeer, pivotRecorder, chunk.Address(), chunk.Data())

	// Pivot peer will forward the chunk to its closest peer. Intercept the incoming stream from pivot node and check
	// for the correctness of the chunk
	waitOnRecordAndTest(t, closestPeer, closestRecorder, chunk.Address(), chunk.Data())

	// Similarly intercept the same incoming stream to see if the closest peer is sending a proper receipt
	waitOnRecordAndTest(t, closestPeer, closestRecorder, chunk.Address(), nil)

	// In the received stream, check if a receipt is sent from pivot peer and check for its correctness.
	waitOnRecordAndTest(t, pivotPeer, pivotRecorder, chunk.Address(), nil)

	balance, err := triggerAccounting.Balance(pivotPeer)
	if err != nil {
		t.Fatal(err)
	}

	if balance.Int64() != -int64(fixedPrice) {
		t.Fatalf("unexpected balance on trigger. want %d got %d", -int64(fixedPrice), balance)
	}

	balance, err = pivotAccounting.Balance(triggerPeer)
	if err != nil {
		t.Fatal(err)
	}

	if balance.Int64() != int64(fixedPrice) {
		t.Fatalf("unexpected balance on pivot. want %d got %d", int64(fixedPrice), balance)
	}

	balance, err = pivotAccounting.Balance(closestPeer)
	if err != nil {
		t.Fatal(err)
	}

	if balance.Int64() != -int64(fixedPrice) {
		t.Fatalf("unexpected balance on pivot. want %d got %d", -int64(fixedPrice), balance)
	}

	balance, err = closestAccounting.Balance(pivotPeer)
	if err != nil {
		t.Fatal(err)
	}

	if balance.Int64() != int64(fixedPrice) {
		t.Fatalf("unexpected balance on closest. want %d got %d", int64(fixedPrice), balance)
	}
}

func createPushSyncNode(t *testing.T, addr infinity.Address, recorder *streamtest.Recorder, unwrap func(infinity.Chunk), mockOpts ...mock.Option) (*pushsync.PushSync, *localstore.DB, *tags.Tags, accounting.Interface) {
	t.Helper()
	logger := logging.New(ioutil.Discard, 0)

	storer, err := localstore.New("", addr.Bytes(), nil, logger)
	if err != nil {
		t.Fatal(err)
	}

	mockTopology := mock.NewTopologyDriver(mockOpts...)
	mockStatestore := statestore.NewStateStore()
	mtag := tags.NewTags(mockStatestore, logger)
	mockAccounting := accountingmock.NewAccounting()
	mockPricer := accountingmock.NewPricer(fixedPrice, fixedPrice)

	recorderDisconnecter := streamtest.NewRecorderDisconnecter(recorder)
	if unwrap == nil {
		unwrap = func(infinity.Chunk) {}
	}

	return pushsync.New(recorderDisconnecter, storer, mockTopology, mtag, unwrap, logger, mockAccounting, mockPricer, nil), storer, mtag, mockAccounting
}

func waitOnRecordAndTest(t *testing.T, peer infinity.Address, recorder *streamtest.Recorder, add infinity.Address, data []byte) {
	t.Helper()
	records := recorder.WaitRecords(t, peer, pushsync.ProtocolName, pushsync.ProtocolVersion, pushsync.StreamName, 1, 5)

	if data != nil {
		messages, err := protobuf.ReadMessages(
			bytes.NewReader(records[0].In()),
			func() protobuf.Message { return new(pb.Delivery) },
		)
		if err != nil {
			t.Fatal(err)
		}
		if messages == nil {
			t.Fatal("nil rcvd. for message")
		}
		if len(messages) > 1 {
			t.Fatal("too many messages")
		}
		delivery := messages[0].(*pb.Delivery)

		if !bytes.Equal(delivery.Address, add.Bytes()) {
			t.Fatalf("chunk address mismatch")
		}

		if !bytes.Equal(delivery.Data, data) {
			t.Fatalf("chunk data mismatch")
		}
	} else {
		messages, err := protobuf.ReadMessages(
			bytes.NewReader(records[0].In()),
			func() protobuf.Message { return new(pb.Receipt) },
		)
		if err != nil {
			t.Fatal(err)
		}
		if messages == nil {
			t.Fatal("nil rcvd. for message")
		}
		if len(messages) > 1 {
			t.Fatal("too many messages")
		}
		receipt := messages[0].(*pb.Receipt)
		receiptAddress := infinity.NewAddress(receipt.Address)

		if !receiptAddress.Equal(add) {
			t.Fatalf("receipt address mismatch")
		}
	}
}

func chanFunc(c chan<- struct{}) func(infinity.Chunk) {
	return func(_ infinity.Chunk) {
		c <- struct{}{}
	}
}
