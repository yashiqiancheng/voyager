// Copyright 2020 The Smart Chain Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package kademlia_test

import (
	"context"
	"errors"
	"io/ioutil"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"

	ma "github.com/multiformats/go-multiaddr"

	"github.com/yanhuangpai/voyager/pkg/addressbook"
	"github.com/yanhuangpai/voyager/pkg/crypto"
	voyagerCrypto "github.com/yanhuangpai/voyager/pkg/crypto"
	"github.com/yanhuangpai/voyager/pkg/discovery/mock"
	"github.com/yanhuangpai/voyager/pkg/ifi"
	"github.com/yanhuangpai/voyager/pkg/infinity"
	"github.com/yanhuangpai/voyager/pkg/infinity/test"
	"github.com/yanhuangpai/voyager/pkg/kademlia"
	"github.com/yanhuangpai/voyager/pkg/kademlia/pslice"
	"github.com/yanhuangpai/voyager/pkg/logging"
	"github.com/yanhuangpai/voyager/pkg/p2p"
	p2pmock "github.com/yanhuangpai/voyager/pkg/p2p/mock"
	mockstate "github.com/yanhuangpai/voyager/pkg/statestore/mock"
	"github.com/yanhuangpai/voyager/pkg/topology"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

var nonConnectableAddress, _ = ma.NewMultiaddr(underlayBase + "16Uiu2HAkx8ULY8cTXhdVAcMmLcH9AsTKz6uBQ7DPLKRjMLgBVYkA")

// TestNeighborhoodDepth tests that the kademlia depth changes correctly
// according to the change to known peers slice. This inadvertently tests
// the functionality in `manage()` method, however this is not the main aim of the
// test, since depth calculation happens there and in the disconnect method.
// A more in depth testing of the functionality in `manage()` is explicitly
// tested in TestManage below.
func TestNeighborhoodDepth(t *testing.T) {
	var (
		conns                    int32 // how many connect calls were made to the p2p mock
		base, kad, ab, _, signer = newTestKademlia(&conns, nil, kademlia.Options{})
		peers                    []infinity.Address
		binEight                 []infinity.Address
	)

	if err := kad.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer kad.Close()

	for i := 0; i < 8; i++ {
		addr := test.RandomAddressAt(base, i)
		peers = append(peers, addr)
	}

	for i := 0; i < 2; i++ {
		addr := test.RandomAddressAt(base, 8)
		binEight = append(binEight, addr)
	}

	// check empty kademlia depth is 0
	kDepth(t, kad, 0)

	// add two bin 8 peers, verify depth still 0
	add(t, signer, kad, ab, binEight, 0, 2)
	kDepth(t, kad, 0)

	// add two first peers (po0,po1)
	add(t, signer, kad, ab, peers, 0, 2)

	// wait for 4 connections
	waitCounter(t, &conns, 4)

	// depth 2 (shallowest empty bin)
	kDepth(t, kad, 2)

	for i := 2; i < len(peers)-1; i++ {
		addOne(t, signer, kad, ab, peers[i])

		// wait for one connection
		waitConn(t, &conns)

		// depth is i+1
		kDepth(t, kad, i+1)
	}

	// the last peer in bin 7 which is empty we insert manually,
	addOne(t, signer, kad, ab, peers[len(peers)-1])
	waitConn(t, &conns)

	// depth is 8 because we have nnLowWatermark neighbors in bin 8
	kDepth(t, kad, 8)

	// now add another ONE peer at depth+1, and expect the depth to still
	// stay 8, because the counter for nnLowWatermark would be reached only at the next
	// depth iteration when calculating depth
	addr := test.RandomAddressAt(base, 9)
	addOne(t, signer, kad, ab, addr)
	waitConn(t, &conns)
	kDepth(t, kad, 8)

	// fill the rest up to the bin before last and check that everything works at the edges
	for i := 10; i < int(infinity.MaxBins)-1; i++ {
		addr := test.RandomAddressAt(base, i)
		addOne(t, signer, kad, ab, addr)
		waitConn(t, &conns)
		kDepth(t, kad, i-1)
	}

	// add a whole bunch of peers in bin 13, expect depth to stay at 13
	for i := 0; i < 15; i++ {
		addr = test.RandomAddressAt(base, 13)
		addOne(t, signer, kad, ab, addr)
	}

	waitCounter(t, &conns, 15)
	kDepth(t, kad, 13)

	// add one at 14 - depth should be now 14
	addr = test.RandomAddressAt(base, 14)
	addOne(t, signer, kad, ab, addr)
	kDepth(t, kad, 14)

	addr2 := test.RandomAddressAt(base, 15)
	addOne(t, signer, kad, ab, addr2)
	kDepth(t, kad, 14)

	addr3 := test.RandomAddressAt(base, 15)
	addOne(t, signer, kad, ab, addr3)
	kDepth(t, kad, 15)

	// now remove that peer and check that the depth is back at 14
	removeOne(kad, addr3)
	kDepth(t, kad, 14)

	// remove the peer at bin 1, depth should be 1
	removeOne(kad, peers[1])
	kDepth(t, kad, 1)
}

// TestManage explicitly tests that new connections are made according to
// the addition or subtraction of peers to the knownPeers and connectedPeers
// data structures. It tests that kademlia will try to initiate (emphesis on _initiate_,
// since right now this test does not test for a mark-and-sweep behaviour of kademlia
// that will prune or disconnect old or less performent nodes when a certain condition
// in a bin has voyagern met - these are future optimizations that still need be sketched out)
// connections when a certain bin is _not_ saturated, and that kademlia does _not_ try
// to initiate connections on a saturated bin.
// Saturation from the local node's perspective means whether a bin has enough connections
// on a given bin.
// What Saturation does _not_ mean: that all nodes are performent, that all nodes we know of
// in a given bin are connected (since some of them might be offline)
func TestManage(t *testing.T) {
	var (
		conns int32 // how many connect calls were made to the p2p mock

		saturationVal     = false
		overSaturationVal = false
		saturationFunc    = func(bin uint8, peers, connected *pslice.PSlice) (bool, bool) {
			return saturationVal, overSaturationVal
		}
		base, kad, ab, _, signer = newTestKademlia(&conns, nil, kademlia.Options{BitSuffixLength: -1, SaturationFunc: saturationFunc})
	)

	if err := kad.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer kad.Close()
	// first, saturationFunc returns always false, this means that the bin is not saturated,
	// hence we expect that every peer we add to kademlia will be connected to
	for i := 0; i < 50; i++ {
		addr := test.RandomAddressAt(base, 0)
		addOne(t, signer, kad, ab, addr)
	}

	waitCounter(t, &conns, 50)
	saturationVal = true

	// now since the bin is "saturated", no new connections should be made
	for i := 0; i < 50; i++ {
		addr := test.RandomAddressAt(base, 0)
		addOne(t, signer, kad, ab, addr)
	}

	waitCounter(t, &conns, 0)

	// check other bins just for fun
	for i := 0; i < 16; i++ {
		for j := 0; j < 10; j++ {
			addr := test.RandomAddressAt(base, i)
			addOne(t, signer, kad, ab, addr)
		}
	}
	waitCounter(t, &conns, 0)
}

func TestManageWithBalancing(t *testing.T) {
	// use "fixed" seed for this
	rand.Seed(2)

	var (
		conns int32 // how many connect calls were made to the p2p mock

		saturationFuncImpl *func(bin uint8, peers, connected *pslice.PSlice) (bool, bool)
		saturationFunc     = func(bin uint8, peers, connected *pslice.PSlice) (bool, bool) {
			f := *saturationFuncImpl
			return f(bin, peers, connected)
		}
		base, kad, ab, _, signer = newTestKademlia(&conns, nil, kademlia.Options{SaturationFunc: saturationFunc, BitSuffixLength: 2})
	)

	// implement satiration function (while having access to Kademlia instance)
	sfImpl := func(bin uint8, peers, connected *pslice.PSlice) (bool, bool) {
		return kad.IsBalanced(bin), false
	}
	saturationFuncImpl = &sfImpl

	if err := kad.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer kad.Close()

	// add peers for bin '0', enough to have balanced connections
	for i := 0; i < 20; i++ {
		addr := test.RandomAddressAt(base, 0)
		addOne(t, signer, kad, ab, addr)
	}

	waitBalanced(t, kad, 0)

	// add peers for other bins, enough to have balanced connections
	for i := 1; i <= int(infinity.MaxPO); i++ {
		for j := 0; j < 20; j++ {
			addr := test.RandomAddressAt(base, i)
			addOne(t, signer, kad, ab, addr)
		}
		// sanity check depth
		kDepth(t, kad, i)
	}

	// Without introducing ExtendedPO / ExtendedProximity, we could only have balanced connections until a depth of 12
	// That is because, the proximity expected for a balanced connection is Bin + 1 + suffix length
	// But, Proximity(one, other) is limited to return MaxPO.
	// So, when we get to 1 + suffix length near MaxPO, our expected proximity is not returned,
	// even if the addresses match in the expected number of bits, because of the MaxPO limiting
	// Without extendedPO, suffix length is 2, + 1 = 3, MaxPO is 15,
	// so we could only have balanced connections for up until bin 12, but not bin 13,
	// as we would be expecting proximity of pseudoaddress-balancedConnection as 16 and get 15 only

	for i := 1; i <= int(infinity.MaxPO); i++ {
		waitBalanced(t, kad, uint8(i))
	}

}

// TestBinSaturation tests the builtin binSaturated function.
// the test must have two phases of adding peers so that the section
// beyond the first flow control statement gets hit (if po >= depth),
// meaning, on the first iteration we add peer and this condition will always
// be true since depth is increasingly moving deeper, but then we add more peers
// in shallower depth for the rest of the function to be executed
func TestBinSaturation(t *testing.T) {
	defer func(p int) {
		*kademlia.SaturationPeers = p
	}(*kademlia.SaturationPeers)
	*kademlia.SaturationPeers = 2

	var (
		conns                    int32 // how many connect calls were made to the p2p mock
		base, kad, ab, _, signer = newTestKademlia(&conns, nil, kademlia.Options{BitSuffixLength: -1})
		peers                    []infinity.Address
	)

	if err := kad.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer kad.Close()

	// add two peers in a few bins to generate some depth >= 0, this will
	// make the next iteration result in binSaturated==true, causing no new
	// connections to be made
	for i := 0; i < 5; i++ {
		for j := 0; j < 2; j++ {
			addr := test.RandomAddressAt(base, i)
			addOne(t, signer, kad, ab, addr)
			peers = append(peers, addr)
		}
	}
	waitCounter(t, &conns, 10)

	// add one more peer in each bin shallower than depth and
	// expect no connections due to saturation. if we add a peer within
	// depth, the short circuit will be hit and we will connect to the peer
	for i := 0; i < 4; i++ {
		addr := test.RandomAddressAt(base, i)
		addOne(t, signer, kad, ab, addr)
	}
	waitCounter(t, &conns, 0)

	// add one peer in a bin higher (unsaturated) and expect one connection
	addr := test.RandomAddressAt(base, 6)
	addOne(t, signer, kad, ab, addr)

	waitCounter(t, &conns, 1)

	// again, one bin higher
	addr = test.RandomAddressAt(base, 7)
	addOne(t, signer, kad, ab, addr)

	waitCounter(t, &conns, 1)

	// this is in order to hit the `if size < 2` in the saturation func
	removeOne(kad, peers[2])
	waitCounter(t, &conns, 1)
}

func TestOversaturation(t *testing.T) {
	defer func(p int) {
		*kademlia.OverSaturationPeers = p
	}(*kademlia.OverSaturationPeers)
	*kademlia.OverSaturationPeers = 8

	var (
		conns                    int32 // how many connect calls were made to the p2p mock
		base, kad, ab, _, signer = newTestKademlia(&conns, nil, kademlia.Options{})
	)

	if err := kad.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer kad.Close()

	// Add maximum accepted number of peers up until bin 5 without problems
	for i := 0; i < 6; i++ {
		for j := 0; j < *kademlia.OverSaturationPeers; j++ {
			addr := test.RandomAddressAt(base, i)
			// if error is not nil as specified, connectOne goes fatal
			connectOne(t, signer, kad, ab, addr, nil)
		}
		// see depth is limited to currently added peers proximity
		kDepth(t, kad, i)
	}

	// see depth is 5
	kDepth(t, kad, 5)

	for k := 0; k < 5; k++ {
		// no further connections can be made
		for l := 0; l < 3; l++ {
			addr := test.RandomAddressAt(base, k)
			// if error is not as specified, connectOne goes fatal
			connectOne(t, signer, kad, ab, addr, topology.ErrOversaturated)
			// check that pick works correctly
			if kad.Pick(p2p.Peer{Address: addr}) {
				t.Fatal("should not pick the peer")
			}
		}
		// see depth is still as expected
		kDepth(t, kad, 5)
	}

	// see we can still add / not limiting more peers in neighborhood depth
	for m := 0; m < 12; m++ {
		addr := test.RandomAddressAt(base, 5)
		// if error is not nil as specified, connectOne goes fatal
		connectOne(t, signer, kad, ab, addr, nil)
		// see depth is still as expected
		kDepth(t, kad, 5)
	}
}

func TestOversaturationBootnode(t *testing.T) {
	defer func(p int) {
		*kademlia.OverSaturationPeers = p
	}(*kademlia.OverSaturationPeers)
	*kademlia.OverSaturationPeers = 4

	var (
		conns                    int32 // how many connect calls were made to the p2p mock
		base, kad, ab, _, signer = newTestKademlia(&conns, nil, kademlia.Options{BootnodeMode: true})
	)

	if err := kad.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer kad.Close()

	// Add maximum accepted number of peers up until bin 5 without problems
	for i := 0; i < 6; i++ {
		for j := 0; j < *kademlia.OverSaturationPeers; j++ {
			addr := test.RandomAddressAt(base, i)
			// if error is not nil as specified, connectOne goes fatal
			connectOne(t, signer, kad, ab, addr, nil)
		}
		// see depth is limited to currently added peers proximity
		kDepth(t, kad, i)
	}

	// see depth is 5
	kDepth(t, kad, 5)

	for k := 0; k < 5; k++ {
		// further connections should succeed outside of depth
		for l := 0; l < 3; l++ {
			addr := test.RandomAddressAt(base, k)
			// if error is not as specified, connectOne goes fatal
			connectOne(t, signer, kad, ab, addr, nil)
			// check that pick works correctly
			if !kad.Pick(p2p.Peer{Address: addr}) {
				t.Fatal("should pick the peer but didnt")
			}
		}
		// see depth is still as expected
		kDepth(t, kad, 5)
	}

	// see we can still add / not limiting more peers in neighborhood depth
	for m := 0; m < 12; m++ {
		addr := test.RandomAddressAt(base, 5)
		// if error is not nil as specified, connectOne goes fatal
		connectOne(t, signer, kad, ab, addr, nil)
		// see depth is still as expected
		kDepth(t, kad, 5)
	}
}

// TestNotifierHooks tests that the Connected/Disconnected hooks
// result in the correct behavior once called.
func TestNotifierHooks(t *testing.T) {
	t.Skip("disabled due to kademlia inconsistencies hotfix")
	var (
		base, kad, ab, _, signer = newTestKademlia(nil, nil, kademlia.Options{})
		peer                     = test.RandomAddressAt(base, 3)
		addr                     = test.RandomAddressAt(peer, 4) // address which is closer to peer
	)

	if err := kad.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer kad.Close()

	connectOne(t, signer, kad, ab, peer, nil)

	p, err := kad.ClosestPeer(addr)
	if err != nil {
		t.Fatal(err)
	}

	if !p.Equal(peer) {
		t.Fatal("got wrong peer address")
	}

	// disconnect the peer, expect error
	kad.Disconnected(p2p.Peer{Address: peer})
	_, err = kad.ClosestPeer(addr)
	if !errors.Is(err, topology.ErrNotFound) {
		t.Fatalf("expected topology.ErrNotFound but got %v", err)
	}
}

// TestDiscoveryHooks check that a peer is gossiped to other peers
// once we establish a connection to this peer. This could be as a result of
// us proactively dialing in to a peer, or when a peer dials in.
func TestDiscoveryHooks(t *testing.T) {
	var (
		conns                    int32
		_, kad, ab, disc, signer = newTestKademlia(&conns, nil, kademlia.Options{})
		p1, p2, p3               = test.RandomAddress(), test.RandomAddress(), test.RandomAddress()
	)

	if err := kad.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer kad.Close()

	// first add a peer from AddPeers, wait for the connection
	addOne(t, signer, kad, ab, p1)
	waitConn(t, &conns)
	// add another peer from AddPeers, wait for the connection
	// then check that peers are gossiped to each other via discovery
	addOne(t, signer, kad, ab, p2)
	waitConn(t, &conns)
	waitBcast(t, disc, p1, p2)
	waitBcast(t, disc, p2, p1)

	disc.Reset()

	// add another peer that dialed in, check that all peers gossiped
	// correctly to each other
	connectOne(t, signer, kad, ab, p3, nil)
	waitBcast(t, disc, p1, p3)
	waitBcast(t, disc, p2, p3)
	waitBcast(t, disc, p3, p1, p2)
}

func TestBackoff(t *testing.T) {
	// cheat and decrease the timer
	defer func(t time.Duration) {
		*kademlia.TimeToRetry = t
	}(*kademlia.TimeToRetry)

	*kademlia.TimeToRetry = 500 * time.Millisecond

	var (
		conns                    int32 // how many connect calls were made to the p2p mock
		base, kad, ab, _, signer = newTestKademlia(&conns, nil, kademlia.Options{})
	)

	if err := kad.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer kad.Close()

	// add one peer, wait for connection
	addr := test.RandomAddressAt(base, 1)
	addOne(t, signer, kad, ab, addr)

	waitCounter(t, &conns, 1)

	// remove that peer
	removeOne(kad, addr)

	// wait for 100ms, add another peer, expect just one more connection
	time.Sleep(100 * time.Millisecond)
	addr = test.RandomAddressAt(base, 1)
	addOne(t, signer, kad, ab, addr)

	waitCounter(t, &conns, 1)

	// wait for another 400ms, add another, expect 2 connections
	time.Sleep(400 * time.Millisecond)
	addr = test.RandomAddressAt(base, 1)
	addOne(t, signer, kad, ab, addr)

	waitCounter(t, &conns, 2)
}

func TestAddressBookPrune(t *testing.T) {
	// test pruning addressbook after successive failed connect attempts
	// cheat and decrease the timer
	defer func(t time.Duration) {
		*kademlia.TimeToRetry = t
	}(*kademlia.TimeToRetry)

	*kademlia.TimeToRetry = 50 * time.Millisecond

	var (
		conns, failedConns       int32 // how many connect calls were made to the p2p mock
		base, kad, ab, _, signer = newTestKademlia(&conns, &failedConns, kademlia.Options{})
	)

	if err := kad.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer kad.Close()

	nonConnPeer, err := ifi.NewAddress(signer, nonConnectableAddress, test.RandomAddressAt(base, 1), 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := ab.Put(nonConnPeer.Overlay, *nonConnPeer); err != nil {
		t.Fatal(err)
	}

	// add non connectable peer, check connection and failed connection counters
	_ = kad.AddPeers(context.Background(), nonConnPeer.Overlay)
	waitCounter(t, &conns, 0)
	waitCounter(t, &failedConns, 1)

	addr := test.RandomAddressAt(base, 1)
	addr1 := test.RandomAddressAt(base, 1)
	addr2 := test.RandomAddressAt(base, 1)

	p, err := ab.Get(nonConnPeer.Overlay)
	if err != nil {
		t.Fatal(err)
	}

	if !nonConnPeer.Equal(p) {
		t.Fatalf("expected %+v, got %+v", nonConnPeer, p)
	}

	time.Sleep(50 * time.Millisecond)
	// add one valid peer to initiate the retry, check connection and failed connection counters
	addOne(t, signer, kad, ab, addr)
	waitCounter(t, &conns, 1)
	waitCounter(t, &failedConns, 1)

	p, err = ab.Get(nonConnPeer.Overlay)
	if err != nil {
		t.Fatal(err)
	}

	if !nonConnPeer.Equal(p) {
		t.Fatalf("expected %+v, got %+v", nonConnPeer, p)
	}

	time.Sleep(50 * time.Millisecond)
	// add one valid peer to initiate the retry, check connection and failed connection counters
	addOne(t, signer, kad, ab, addr1)
	waitCounter(t, &conns, 1)
	waitCounter(t, &failedConns, 1)

	p, err = ab.Get(nonConnPeer.Overlay)
	if err != nil {
		t.Fatal(err)
	}

	if !nonConnPeer.Equal(p) {
		t.Fatalf("expected %+v, got %+v", nonConnPeer, p)
	}

	time.Sleep(50 * time.Millisecond)
	// add one valid peer to initiate the retry, check connection and failed connection counters
	addOne(t, signer, kad, ab, addr2)
	waitCounter(t, &conns, 1)
	waitCounter(t, &failedConns, 1)

	_, err = ab.Get(nonConnPeer.Overlay)
	if err != addressbook.ErrNotFound {
		t.Fatal(err)
	}
}

// TestClosestPeer tests that ClosestPeer method returns closest connected peer to a given address.
func TestClosestPeer(t *testing.T) {
	_ = waitPeers
	t.Skip("disabled due to kademlia inconsistencies hotfix")

	logger := logging.New(ioutil.Discard, 0)
	base := infinity.MustParseHexAddress("0000000000000000000000000000000000000000000000000000000000000000") // base is 0000
	connectedPeers := []p2p.Peer{
		{
			Address: infinity.MustParseHexAddress("8000000000000000000000000000000000000000000000000000000000000000"), // binary 1000 -> po 0 to base
		},
		{
			Address: infinity.MustParseHexAddress("4000000000000000000000000000000000000000000000000000000000000000"), // binary 0100 -> po 1 to base
		},
		{
			Address: infinity.MustParseHexAddress("6000000000000000000000000000000000000000000000000000000000000000"), // binary 0110 -> po 1 to base
		},
	}

	disc := mock.NewDiscovery()
	ab := addressbook.New(mockstate.NewStateStore())

	kad := kademlia.New(base, ab, disc, p2pMock(ab, nil, nil, nil), logger, kademlia.Options{})
	if err := kad.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer kad.Close()

	pk, _ := crypto.GenerateSecp256k1Key()
	for _, v := range connectedPeers {
		addOne(t, voyagerCrypto.NewDefaultSigner(pk), kad, ab, v.Address)
	}

	waitPeers(t, kad, 3)

	for _, tc := range []struct {
		chunkAddress infinity.Address // chunk address to test
		expectedPeer int              // points to the index of the connectedPeers slice. -1 means self (baseOverlay)
	}{
		{
			chunkAddress: infinity.MustParseHexAddress("7000000000000000000000000000000000000000000000000000000000000000"), // 0111, wants peer 2
			expectedPeer: 2,
		},
		{
			chunkAddress: infinity.MustParseHexAddress("c000000000000000000000000000000000000000000000000000000000000000"), // 1100, want peer 0
			expectedPeer: 0,
		},
		{
			chunkAddress: infinity.MustParseHexAddress("e000000000000000000000000000000000000000000000000000000000000000"), // 1110, want peer 0
			expectedPeer: 0,
		},
		{
			chunkAddress: infinity.MustParseHexAddress("a000000000000000000000000000000000000000000000000000000000000000"), // 1010, want peer 0
			expectedPeer: 0,
		},
		{
			chunkAddress: infinity.MustParseHexAddress("4000000000000000000000000000000000000000000000000000000000000000"), // 0100, want peer 1
			expectedPeer: 1,
		},
		{
			chunkAddress: infinity.MustParseHexAddress("5000000000000000000000000000000000000000000000000000000000000000"), // 0101, want peer 1
			expectedPeer: 1,
		},
		{
			chunkAddress: infinity.MustParseHexAddress("0000001000000000000000000000000000000000000000000000000000000000"), // want self
			expectedPeer: -1,
		},
	} {
		peer, err := kad.ClosestPeer(tc.chunkAddress)
		if err != nil {
			if tc.expectedPeer == -1 && !errors.Is(err, topology.ErrWantSelf) {
				t.Fatalf("wanted %v but got %v", topology.ErrWantSelf, err)
			}
			continue
		}

		expected := connectedPeers[tc.expectedPeer].Address

		if !peer.Equal(expected) {
			t.Fatalf("peers not equal. got %s expected %s", peer, expected)
		}
	}
}

func TestKademlia_SubscribePeersChange(t *testing.T) {
	testSignal := func(t *testing.T, k *kademlia.Kad, c <-chan struct{}) {
		t.Helper()

		select {
		case _, ok := <-c:
			if !ok {
				t.Error("closed signal channel")
			}
		case <-time.After(1 * time.Second):
			t.Error("timeout")
		}
	}

	t.Run("single subscription", func(t *testing.T) {
		base, kad, ab, _, sg := newTestKademlia(nil, nil, kademlia.Options{})
		if err := kad.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer kad.Close()

		c, u := kad.SubscribePeersChange()
		defer u()

		addr := test.RandomAddressAt(base, 9)
		addOne(t, sg, kad, ab, addr)

		testSignal(t, kad, c)
	})

	t.Run("single subscription, remove peer", func(t *testing.T) {
		base, kad, ab, _, sg := newTestKademlia(nil, nil, kademlia.Options{})
		if err := kad.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer kad.Close()

		c, u := kad.SubscribePeersChange()
		defer u()

		addr := test.RandomAddressAt(base, 9)
		addOne(t, sg, kad, ab, addr)

		testSignal(t, kad, c)

		removeOne(kad, addr)
		testSignal(t, kad, c)
	})

	t.Run("multiple subscriptions", func(t *testing.T) {
		base, kad, ab, _, sg := newTestKademlia(nil, nil, kademlia.Options{})
		if err := kad.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer kad.Close()

		c1, u1 := kad.SubscribePeersChange()
		defer u1()

		c2, u2 := kad.SubscribePeersChange()
		defer u2()

		for i := 0; i < 4; i++ {
			addr := test.RandomAddressAt(base, i)
			addOne(t, sg, kad, ab, addr)
		}
		testSignal(t, kad, c1)
		testSignal(t, kad, c2)
	})

	t.Run("multiple changes", func(t *testing.T) {
		base, kad, ab, _, sg := newTestKademlia(nil, nil, kademlia.Options{})
		if err := kad.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer kad.Close()

		c, u := kad.SubscribePeersChange()
		defer u()

		for i := 0; i < 4; i++ {
			addr := test.RandomAddressAt(base, i)
			addOne(t, sg, kad, ab, addr)
		}

		testSignal(t, kad, c)

		for i := 0; i < 4; i++ {
			addr := test.RandomAddressAt(base, i)
			addOne(t, sg, kad, ab, addr)
		}

		testSignal(t, kad, c)
	})

	t.Run("no depth change", func(t *testing.T) {
		_, kad, _, _, _ := newTestKademlia(nil, nil, kademlia.Options{})
		if err := kad.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer kad.Close()

		c, u := kad.SubscribePeersChange()
		defer u()

		select {
		case _, ok := <-c:
			if !ok {
				t.Error("closed signal channel")
			}
			t.Error("signal received")
		case <-time.After(1 * time.Second):
			// all fine
		}
	})
}

func TestMarshal(t *testing.T) {
	_, kad, ab, _, signer := newTestKademlia(nil, nil, kademlia.Options{})
	if err := kad.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer kad.Close()

	a := test.RandomAddress()
	addOne(t, signer, kad, ab, a)
	_, err := kad.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
}

func TestStart(t *testing.T) {
	var bootnodes []ma.Multiaddr
	for i := 0; i < 10; i++ {
		multiaddr, err := ma.NewMultiaddr(underlayBase + test.RandomAddress().String())
		if err != nil {
			t.Fatal(err)
		}

		bootnodes = append(bootnodes, multiaddr)
	}

	t.Run("non-empty addressbook", func(t *testing.T) {
		var conns, failedConns int32 // how many connect calls were made to the p2p mock
		_, kad, ab, _, signer := newTestKademlia(&conns, &failedConns, kademlia.Options{Bootnodes: bootnodes})
		defer kad.Close()

		for i := 0; i < 3; i++ {
			peer := test.RandomAddress()
			multiaddr, err := ma.NewMultiaddr(underlayBase + peer.String())
			if err != nil {
				t.Fatal(err)
			}
			ifiAddr, err := ifi.NewAddress(signer, multiaddr, peer, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := ab.Put(peer, *ifiAddr); err != nil {
				t.Fatal(err)
			}
		}

		if err := kad.Start(context.Background()); err != nil {
			t.Fatal(err)
		}

		waitCounter(t, &conns, 3)
		waitCounter(t, &failedConns, 0)
	})

	t.Run("empty addressbook", func(t *testing.T) {
		var conns, failedConns int32 // how many connect calls were made to the p2p mock
		_, kad, _, _, _ := newTestKademlia(&conns, &failedConns, kademlia.Options{Bootnodes: bootnodes})
		defer kad.Close()

		if err := kad.Start(context.Background()); err != nil {
			t.Fatal(err)
		}

		waitCounter(t, &conns, 3)
		waitCounter(t, &failedConns, 0)
	})
}

func newTestKademlia(connCounter, failedConnCounter *int32, kadOpts kademlia.Options) (infinity.Address, *kademlia.Kad, addressbook.Interface, *mock.Discovery, voyagerCrypto.Signer) {
	var (
		pk, _  = crypto.GenerateSecp256k1Key()                       // random private key
		signer = voyagerCrypto.NewDefaultSigner(pk)                  // signer
		base   = test.RandomAddress()                                // base address
		ab     = addressbook.New(mockstate.NewStateStore())          // address book
		p2p    = p2pMock(ab, signer, connCounter, failedConnCounter) // p2p mock
		logger = logging.New(ioutil.Discard, 0)                      // logger
		disc   = mock.NewDiscovery()                                 // mock discovery protocol
		kad    = kademlia.New(base, ab, disc, p2p, logger, kadOpts)  // kademlia instance
	)

	return base, kad, ab, disc, signer
}

func p2pMock(ab addressbook.Interface, signer voyagerCrypto.Signer, counter, failedCounter *int32) p2p.Service {
	p2ps := p2pmock.New(p2pmock.WithConnectFunc(func(ctx context.Context, addr ma.Multiaddr) (*ifi.Address, error) {
		if addr.Equal(nonConnectableAddress) {
			_ = atomic.AddInt32(failedCounter, 1)
			return nil, errors.New("non reachable node")
		}
		if counter != nil {
			_ = atomic.AddInt32(counter, 1)
		}

		addresses, err := ab.Addresses()
		if err != nil {
			return nil, errors.New("could not fetch addresbook addresses")
		}

		for _, a := range addresses {
			if a.Underlay.Equal(addr) {
				return &a, nil
			}
		}

		address := test.RandomAddress()
		ifiAddr, err := ifi.NewAddress(signer, addr, address, 0)
		if err != nil {
			return nil, err
		}

		if err := ab.Put(address, *ifiAddr); err != nil {
			return nil, err
		}

		return ifiAddr, nil
	}))

	return p2ps
}

func removeOne(k *kademlia.Kad, peer infinity.Address) {
	k.Disconnected(p2p.Peer{Address: peer})
}

const underlayBase = "/ip4/127.0.0.1/tcp/11634/dns/"

func connectOne(t *testing.T, signer voyagerCrypto.Signer, k *kademlia.Kad, ab addressbook.Putter, peer infinity.Address, expErr error) {
	t.Helper()
	multiaddr, err := ma.NewMultiaddr(underlayBase + peer.String())
	if err != nil {
		t.Fatal(err)
	}

	ifiAddr, err := ifi.NewAddress(signer, multiaddr, peer, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := ab.Put(peer, *ifiAddr); err != nil {
		t.Fatal(err)
	}
	err = k.Connected(context.Background(), p2p.Peer{Address: peer})

	if !errors.Is(err, expErr) {
		t.Fatalf("expected error %v , got %v", expErr, err)
	}

}

func addOne(t *testing.T, signer voyagerCrypto.Signer, k *kademlia.Kad, ab addressbook.Putter, peer infinity.Address) {
	t.Helper()
	multiaddr, err := ma.NewMultiaddr(underlayBase + peer.String())
	if err != nil {
		t.Fatal(err)
	}
	ifiAddr, err := ifi.NewAddress(signer, multiaddr, peer, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := ab.Put(peer, *ifiAddr); err != nil {
		t.Fatal(err)
	}
	_ = k.AddPeers(context.Background(), peer)
}

func add(t *testing.T, signer voyagerCrypto.Signer, k *kademlia.Kad, ab addressbook.Putter, peers []infinity.Address, offset, number int) {
	t.Helper()
	for i := offset; i < offset+number; i++ {
		addOne(t, signer, k, ab, peers[i])
	}
}

func kDepth(t *testing.T, k *kademlia.Kad, d int) {
	t.Helper()
	var depth int
	for i := 0; i < 50; i++ {
		depth = int(k.NeighborhoodDepth())
		if depth == d {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for depth. want %d got %d", d, depth)
}

func waitConn(t *testing.T, conns *int32) {
	t.Helper()
	waitCounter(t, conns, 1)
}

// waits for counter for some time. resets the pointer value
// if the correct number  have voyagern reached.
func waitCounter(t *testing.T, conns *int32, exp int32) {
	t.Helper()
	var got int32
	if exp == 0 {
		// sleep for some time before checking for a 0.
		// this gives some time for unwanted counter increments happen

		time.Sleep(50 * time.Millisecond)
	}
	for i := 0; i < 50; i++ {
		if got = atomic.LoadInt32(conns); got == exp {
			atomic.StoreInt32(conns, 0)
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for counter to reach expected value. got %d want %d", got, exp)
}

func waitPeers(t *testing.T, k *kademlia.Kad, peers int) {
	timeout := time.After(3 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for peers")
		default:
		}
		i := 0
		_ = k.EachPeer(func(_ infinity.Address, _ uint8) (bool, bool, error) {
			i++
			return false, false, nil
		})
		if i == peers {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// wait for discovery BroadcastPeers to happen
func waitBcast(t *testing.T, d *mock.Discovery, pivot infinity.Address, addrs ...infinity.Address) {
	t.Helper()
	time.Sleep(50 * time.Millisecond)
	for i := 0; i < 50; i++ {
		if d.Broadcasts() > 0 {
			recs, ok := d.AddresseeRecords(pivot)
			if !ok {
				t.Fatal("got no records for pivot")
			}
			oks := 0
			for _, a := range addrs {
				if !isIn(a, recs) {
					t.Fatalf("address %s not found in discovery records: %s", a, addrs)
				}
				oks++
			}

			if oks == len(addrs) {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for broadcast to happen")
}

func isIn(addr infinity.Address, addrs []infinity.Address) bool {
	for _, v := range addrs {
		if v.Equal(addr) {
			return true
		}
	}
	return false
}

// waitBalanced waits for kademlia to be balanced for specified bin.
func waitBalanced(t *testing.T, k *kademlia.Kad, bin uint8) {
	t.Helper()

	timeout := time.After(3 * time.Second)
	for {
		select {
		case <-timeout:
			t.Fatalf("timed out waiting to be balanced for bin: %d", int(bin))
		default:
		}

		if balanced := k.IsBalanced(bin); balanced {
			return
		}

		time.Sleep(50 * time.Millisecond)
	}
}
