// Copyright 2020 The Smart Chain Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package libp2p_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	libp2ppeer "github.com/libp2p/go-libp2p-core/peer"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/yanhuangpai/voyager/pkg/addressbook"
	"github.com/yanhuangpai/voyager/pkg/infinity"
	"github.com/yanhuangpai/voyager/pkg/p2p"
	"github.com/yanhuangpai/voyager/pkg/p2p/libp2p"
	"github.com/yanhuangpai/voyager/pkg/p2p/libp2p/internal/handshake"
	"github.com/yanhuangpai/voyager/pkg/statestore/mock"
)

func TestAddresses(t *testing.T) {
	s, _ := newService(t, 1, libp2pServiceOpts{})

	addrs, err := s.Addresses()
	if err != nil {
		t.Fatal(err)
	}
	if l := len(addrs); l == 0 {
		t.Fatal("no addresses")
	}
}

func TestConnectDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s1, overlay1 := newService(t, 1, libp2pServiceOpts{})

	s2, overlay2 := newService(t, 1, libp2pServiceOpts{})

	addr := serviceUnderlayAddress(t, s1)

	ifiAddr, err := s2.Connect(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2, overlay1)
	expectPeersEventually(t, s1, overlay2)

	if err := s2.Disconnect(ifiAddr.Overlay); err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2)
	expectPeersEventually(t, s1)
}

func TestDoubleConnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s1, overlay1 := newService(t, 1, libp2pServiceOpts{})

	s2, overlay2 := newService(t, 1, libp2pServiceOpts{})

	addr := serviceUnderlayAddress(t, s1)

	if _, err := s2.Connect(ctx, addr); err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2, overlay1)
	expectPeersEventually(t, s1, overlay2)

	if _, err := s2.Connect(ctx, addr); !errors.Is(err, p2p.ErrAlreadyConnected) {
		t.Fatalf("expected %s error, got %s error", p2p.ErrAlreadyConnected, err)
	}

	expectPeers(t, s2, overlay1)
	expectPeers(t, s1, overlay2)
}

func TestDoubleDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s1, overlay1 := newService(t, 1, libp2pServiceOpts{})

	s2, overlay2 := newService(t, 1, libp2pServiceOpts{})

	addr := serviceUnderlayAddress(t, s1)

	ifiAddr, err := s2.Connect(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2, overlay1)
	expectPeersEventually(t, s1, overlay2)

	if err := s2.Disconnect(ifiAddr.Overlay); err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2)
	expectPeersEventually(t, s1)

	if err := s2.Disconnect(ifiAddr.Overlay); !errors.Is(err, p2p.ErrPeerNotFound) {
		t.Errorf("got error %v, want %v", err, p2p.ErrPeerNotFound)
	}

	expectPeers(t, s2)
	expectPeersEventually(t, s1)
}

func TestMultipleConnectDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s1, overlay1 := newService(t, 1, libp2pServiceOpts{})

	s2, overlay2 := newService(t, 1, libp2pServiceOpts{})

	addr := serviceUnderlayAddress(t, s1)

	ifiAddr, err := s2.Connect(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2, overlay1)
	expectPeersEventually(t, s1, overlay2)

	if err := s2.Disconnect(ifiAddr.Overlay); err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2)
	expectPeersEventually(t, s1)

	ifiAddr, err = s2.Connect(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2, overlay1)
	expectPeersEventually(t, s1, overlay2)

	if err := s2.Disconnect(ifiAddr.Overlay); err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2)
	expectPeersEventually(t, s1)
}

func TestConnectDisconnectOnAllAddresses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s1, overlay1 := newService(t, 1, libp2pServiceOpts{})

	s2, overlay2 := newService(t, 1, libp2pServiceOpts{})

	addrs, err := s1.Addresses()
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range addrs {
		ifiAddr, err := s2.Connect(ctx, addr)
		if err != nil {
			t.Fatal(err)
		}

		expectPeers(t, s2, overlay1)
		expectPeersEventually(t, s1, overlay2)

		if err := s2.Disconnect(ifiAddr.Overlay); err != nil {
			t.Fatal(err)
		}

		expectPeers(t, s2)
		expectPeersEventually(t, s1)
	}
}

func TestDoubleConnectOnAllAddresses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s1, overlay1 := newService(t, 1, libp2pServiceOpts{})

	addrs, err := s1.Addresses()
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range addrs {
		// creating new remote host for each address
		s2, overlay2 := newService(t, 1, libp2pServiceOpts{})

		if _, err := s2.Connect(ctx, addr); err != nil {
			t.Fatal(err)
		}

		expectPeers(t, s2, overlay1)
		expectPeersEventually(t, s1, overlay2)

		if _, err := s2.Connect(ctx, addr); !errors.Is(err, p2p.ErrAlreadyConnected) {
			t.Fatalf("expected %s error, got %s error", p2p.ErrAlreadyConnected, err)
		}

		expectPeers(t, s2, overlay1)
		expectPeers(t, s1, overlay2)

		if err := s2.Disconnect(overlay1); err != nil {
			t.Fatal(err)
		}

		expectPeers(t, s2)
		expectPeersEventually(t, s1)

		s2.Close()
	}
}

func TestDifferentNetworkIDs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s1, _ := newService(t, 1, libp2pServiceOpts{})

	s2, _ := newService(t, 2, libp2pServiceOpts{})

	addr := serviceUnderlayAddress(t, s1)

	if _, err := s2.Connect(ctx, addr); err == nil {
		t.Fatal("connect attempt should result with an error")
	}

	expectPeers(t, s1)
	expectPeers(t, s2)
}

func TestConnectWithEnabledQUICAndWSTransports(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s1, overlay1 := newService(t, 1, libp2pServiceOpts{
		libp2pOpts: libp2p.Options{
			EnableQUIC: true,
			EnableWS:   true,
		},
	})

	s2, overlay2 := newService(t, 1, libp2pServiceOpts{
		libp2pOpts: libp2p.Options{
			EnableQUIC: true,
			EnableWS:   true,
		},
	})

	addr := serviceUnderlayAddress(t, s1)

	if _, err := s2.Connect(ctx, addr); err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2, overlay1)
	expectPeersEventually(t, s1, overlay2)
}

// TestConnectRepeatHandshake tests if handshake was attempted more then once by the same peer
func TestConnectRepeatHandshake(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s1, overlay1 := newService(t, 1, libp2pServiceOpts{})
	s2, overlay2 := newService(t, 1, libp2pServiceOpts{})
	addr := serviceUnderlayAddress(t, s1)

	_, err := s2.Connect(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2, overlay1)
	expectPeersEventually(t, s1, overlay2)

	info, err := libp2ppeer.AddrInfoFromP2pAddr(addr)
	if err != nil {
		t.Fatal(err)
	}

	stream, err := s2.NewStreamForPeerID(info.ID, handshake.ProtocolName, handshake.ProtocolVersion, handshake.StreamName)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s2.HandshakeService().Handshake(ctx, libp2p.NewStream(stream), info.Addrs[0], info.ID); err == nil {
		t.Fatalf("expected stream error")
	}

	expectPeersEventually(t, s2)
	expectPeersEventually(t, s1)
}

func TestBlocklisting(t *testing.T) {
	s1, overlay1 := newService(t, 1, libp2pServiceOpts{})
	s2, overlay2 := newService(t, 1, libp2pServiceOpts{})

	addr1 := serviceUnderlayAddress(t, s1)
	addr2 := serviceUnderlayAddress(t, s2)

	// s2 connects to s1, thus the notifier on s1 should be called on Connect
	_, err := s2.Connect(context.Background(), addr1)
	if err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2, overlay1)
	expectPeersEventually(t, s1, overlay2)

	if err := s2.Blocklist(overlay1, 0); err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2)
	expectPeersEventually(t, s1)

	// s2 connects to s1, thus the notifier on s1 should be called on Connect
	_, err = s2.Connect(context.Background(), addr1)
	if err == nil {
		t.Fatal("expected error during connection, got nil")
	}

	expectPeers(t, s2)
	expectPeersEventually(t, s1)

	// s2 connects to s1, thus the notifier on s1 should be called on Connect
	_, err = s1.Connect(context.Background(), addr2)
	if err == nil {
		t.Fatal("expected error during connection, got nil")
	}

	expectPeers(t, s1)
	expectPeersEventually(t, s2)
}

func TestTopologyNotifier(t *testing.T) {
	var (
		mtx sync.Mutex
		ctx = context.Background()

		ab1, ab2 = addressbook.New(mock.NewStateStore()), addressbook.New(mock.NewStateStore())

		n1connectedPeer    p2p.Peer
		n1disconnectedPeer p2p.Peer
		n2connectedPeer    p2p.Peer
		n2disconnectedPeer p2p.Peer

		n1c = func(_ context.Context, p p2p.Peer) error {
			mtx.Lock()
			defer mtx.Unlock()
			expectZeroAddress(t, n1connectedPeer.Address) // fail if set more than once
			n1connectedPeer = p
			return nil
		}
		n1d = func(p p2p.Peer) {
			mtx.Lock()
			defer mtx.Unlock()
			n1disconnectedPeer = p
		}

		n2c = func(_ context.Context, p p2p.Peer) error {
			mtx.Lock()
			defer mtx.Unlock()
			expectZeroAddress(t, n2connectedPeer.Address) // fail if set more than once
			n2connectedPeer = p
			return nil
		}
		n2d = func(p p2p.Peer) {
			mtx.Lock()
			defer mtx.Unlock()
			n2disconnectedPeer = p
		}
	)
	notifier1 := mockNotifier(n1c, n1d, true)
	s1, overlay1 := newService(t, 1, libp2pServiceOpts{Addressbook: ab1})
	s1.SetPickyNotifier(notifier1)

	notifier2 := mockNotifier(n2c, n2d, true)
	s2, overlay2 := newService(t, 1, libp2pServiceOpts{Addressbook: ab2})
	s2.SetPickyNotifier(notifier2)

	addr := serviceUnderlayAddress(t, s1)

	// s2 connects to s1, thus the notifier on s1 should be called on Connect
	ifiAddr, err := s2.Connect(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2, overlay1)
	expectPeersEventually(t, s1, overlay2)

	// expect that n1 notifee called with s2 overlay
	waitAddrSet(t, &n1connectedPeer.Address, &mtx, overlay2)

	mtx.Lock()
	expectZeroAddress(t, n1disconnectedPeer.Address, n2connectedPeer.Address, n2disconnectedPeer.Address)
	mtx.Unlock()

	// check address book entries are there
	checkAddressbook(t, ab2, overlay1, addr)

	// s2 disconnects from s1 so s1 disconnect notifiee should be called
	if err := s2.Disconnect(ifiAddr.Overlay); err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s2)
	expectPeersEventually(t, s1)
	waitAddrSet(t, &n1disconnectedPeer.Address, &mtx, overlay2)

	// note that both n1disconnect and n2disconnect callbacks are called after just
	// one disconnect. this is due to the fact the when the libp2p abstraction is explicitly
	// called to disconnect from a peer, it will also notify the topology notifiee, since
	// peer disconnections can also result from components from outside the bound of the
	// topology driver
	mtx.Lock()
	expectZeroAddress(t, n2connectedPeer.Address)
	mtx.Unlock()

	addr2 := serviceUnderlayAddress(t, s2)
	// s1 connects to s2, thus the notifiee on s2 should be called on Connect
	ifiAddr2, err := s1.Connect(ctx, addr2)
	if err != nil {
		t.Fatal(err)
	}

	expectPeers(t, s1, overlay2)
	expectPeersEventually(t, s2, overlay1)
	waitAddrSet(t, &n2connectedPeer.Address, &mtx, overlay1)

	// s1 disconnects from s2 so s2 disconnect notifiee should be called
	if err := s1.Disconnect(ifiAddr2.Overlay); err != nil {
		t.Fatal(err)
	}
	expectPeers(t, s1)
	expectPeersEventually(t, s2)
	waitAddrSet(t, &n2disconnectedPeer.Address, &mtx, overlay1)
}

func TestTopologyOverSaturated(t *testing.T) {
	var (
		mtx sync.Mutex
		ctx = context.Background()

		ab1, ab2 = addressbook.New(mock.NewStateStore()), addressbook.New(mock.NewStateStore())

		n1connectedPeer    p2p.Peer
		n2connectedPeer    p2p.Peer
		n2disconnectedPeer p2p.Peer

		n1c = func(_ context.Context, p p2p.Peer) error {
			mtx.Lock()
			defer mtx.Unlock()
			expectZeroAddress(t, n1connectedPeer.Address) // fail if set more than once
			n1connectedPeer = p
			return nil
		}
		n1d = func(p p2p.Peer) {}

		n2c = func(_ context.Context, p p2p.Peer) error {
			mtx.Lock()
			defer mtx.Unlock()
			expectZeroAddress(t, n2connectedPeer.Address) // fail if set more than once
			n2connectedPeer = p
			return nil
		}
		n2d = func(p p2p.Peer) {
			mtx.Lock()
			defer mtx.Unlock()
			n2disconnectedPeer = p
		}
	)
	//this notifier will not pick the peer
	notifier1 := mockNotifier(n1c, n1d, false)
	s1, overlay1 := newService(t, 1, libp2pServiceOpts{Addressbook: ab1})
	s1.SetPickyNotifier(notifier1)

	notifier2 := mockNotifier(n2c, n2d, false)
	s2, _ := newService(t, 1, libp2pServiceOpts{Addressbook: ab2})
	s2.SetPickyNotifier(notifier2)

	addr := serviceUnderlayAddress(t, s1)

	// s2 connects to s1, thus the notifier on s1 should be called on Connect
	_, err := s2.Connect(ctx, addr)
	if err == nil {
		t.Fatal("expected connect to fail but it didnt")
	}

	expectPeers(t, s1)
	expectPeersEventually(t, s2)

	waitAddrSet(t, &n2disconnectedPeer.Address, &mtx, overlay1)
}

func expectZeroAddress(t *testing.T, addrs ...infinity.Address) {
	t.Helper()
	for i, a := range addrs {
		if !a.Equal(infinity.ZeroAddress) {
			t.Fatalf("address did not equal zero address. index %d", i)
		}
	}
}

func waitAddrSet(t *testing.T, addr *infinity.Address, mtx *sync.Mutex, exp infinity.Address) {
	t.Helper()
	for i := 0; i < 20; i++ {
		mtx.Lock()
		if addr.Equal(exp) {
			mtx.Unlock()
			return
		}
		mtx.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for address to be set")
}

func checkAddressbook(t *testing.T, ab addressbook.Getter, overlay infinity.Address, underlay ma.Multiaddr) {
	t.Helper()
	addr, err := ab.Get(overlay)
	if err != nil {
		t.Fatal(err)
	}
	if !addr.Overlay.Equal(overlay) {
		t.Fatalf("overlay mismatch. got %s want %s", addr.Overlay, overlay)
	}

	if !addr.Underlay.Equal(underlay) {
		t.Fatalf("underlay mismatch. got %s, want %s", addr.Underlay, underlay)
	}
}

type notifiee struct {
	connected    func(context.Context, p2p.Peer) error
	disconnected func(p2p.Peer)
	pick         bool
}

func (n *notifiee) Connected(c context.Context, p p2p.Peer) error {
	return n.connected(c, p)
}

func (n *notifiee) Disconnected(p p2p.Peer) {
	n.disconnected(p)
}

func (n *notifiee) Pick(p p2p.Peer) bool {
	return n.pick
}

func mockNotifier(c cFunc, d dFunc, pick bool) p2p.PickyNotifier {
	return &notifiee{connected: c, disconnected: d, pick: pick}
}

type cFunc func(context.Context, p2p.Peer) error
type dFunc func(p2p.Peer)
