// Copyright 2020 The Smart Chain Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package hive exposes the hive protocol implementation
// which is the discovery protocol used to inform and be
// informed about other peers in the network. It gossips
// about all peers by default and performs no specific
// prioritization about which peers are gossipped to
// others.
package hive

import (
	"context"
	"fmt"
	"time"

	"github.com/yanhuangpai/voyager/pkg/addressbook"
	"github.com/yanhuangpai/voyager/pkg/hive/pb"
	"github.com/yanhuangpai/voyager/pkg/ifi"
	"github.com/yanhuangpai/voyager/pkg/infinity"
	"github.com/yanhuangpai/voyager/pkg/logging"
	"github.com/yanhuangpai/voyager/pkg/p2p"
	"github.com/yanhuangpai/voyager/pkg/p2p/protobuf"
)

const (
	protocolName    = "hive"
	protocolVersion = "1.0.0"
	peersStreamName = "peers"
	messageTimeout  = 1 * time.Minute // maximum allowed time for a message to be read or written.
	maxBatchSize    = 30
)

type Service struct {
	streamer        p2p.Streamer
	addressBook     addressbook.GetPutter
	addPeersHandler func(context.Context, ...infinity.Address) error
	networkID       uint64
	logger          logging.Logger
	metrics         metrics
}

func New(streamer p2p.Streamer, addressbook addressbook.GetPutter, networkID uint64, logger logging.Logger) *Service {
	return &Service{
		streamer:    streamer,
		logger:      logger,
		addressBook: addressbook,
		networkID:   networkID,
		metrics:     newMetrics(),
	}
}

func (s *Service) Protocol() p2p.ProtocolSpec {
	return p2p.ProtocolSpec{
		Name:    protocolName,
		Version: protocolVersion,
		StreamSpecs: []p2p.StreamSpec{
			{
				Name:    peersStreamName,
				Handler: s.peersHandler,
			},
		},
	}
}

func (s *Service) BroadcastPeers(ctx context.Context, addressee infinity.Address, peers ...infinity.Address) error {
	max := maxBatchSize
	s.metrics.BroadcastPeers.Inc()
	s.metrics.BroadcastPeersPeers.Add(float64(len(peers)))

	for len(peers) > 0 {
		if max > len(peers) {
			max = len(peers)
		}
		if err := s.sendPeers(ctx, addressee, peers[:max]); err != nil {
			return err
		}

		peers = peers[max:]
	}

	return nil
}

func (s *Service) SetAddPeersHandler(h func(ctx context.Context, addr ...infinity.Address) error) {
	s.addPeersHandler = h
}

func (s *Service) sendPeers(ctx context.Context, peer infinity.Address, peers []infinity.Address) (err error) {
	s.metrics.BroadcastPeersSends.Inc()
	stream, err := s.streamer.NewStream(ctx, peer, nil, protocolName, protocolVersion, peersStreamName)
	if err != nil {
		return fmt.Errorf("new stream: %w", err)
	}
	defer func() {
		if err != nil {
			_ = stream.Reset()
		} else {
			_ = stream.FullClose()
		}
	}()
	w, _ := protobuf.NewWriterAndReader(stream)
	var peersRequest pb.Peers
	for _, p := range peers {
		addr, err := s.addressBook.Get(p)
		if err != nil {
			if err == addressbook.ErrNotFound {
				s.logger.Debugf("hive broadcast peers: peer not found in the addressbook. Skipping peer %s", p)
				continue
			}
			return err
		}

		peersRequest.Peers = append(peersRequest.Peers, &pb.IfiAddress{
			Overlay:   addr.Overlay.Bytes(),
			Underlay:  addr.Underlay.Bytes(),
			Signature: addr.Signature,
		})
	}

	if err := w.WriteMsgWithContext(ctx, &peersRequest); err != nil {
		return fmt.Errorf("write Peers message: %w", err)
	}

	return nil
}

func (s *Service) peersHandler(ctx context.Context, peer p2p.Peer, stream p2p.Stream) error {
	s.metrics.PeersHandler.Inc()
	_, r := protobuf.NewWriterAndReader(stream)
	ctx, cancel := context.WithTimeout(ctx, messageTimeout)
	defer cancel()
	var peersReq pb.Peers
	if err := r.ReadMsgWithContext(ctx, &peersReq); err != nil {
		_ = stream.Reset()
		return fmt.Errorf("read requestPeers message: %w", err)
	}

	s.metrics.PeersHandlerPeers.Add(float64(len(peersReq.Peers)))

	// close the stream before processing in order to unblock the sending side
	// fullclose is called async because there is no need to wait for confirmation,
	// but we still want to handle not closed stream from the other side to avoid zombie stream
	go stream.FullClose()

	var peers []infinity.Address
	for _, newPeer := range peersReq.Peers {
		ifiAddress, err := ifi.ParseAddress(newPeer.Underlay, newPeer.Overlay, newPeer.Signature, s.networkID)
		if err != nil {
			s.logger.Warningf("skipping peer in response %s: %v", newPeer.String(), err)
			continue
		}

		err = s.addressBook.Put(ifiAddress.Overlay, *ifiAddress)
		if err != nil {
			s.logger.Warningf("skipping peer in response %s: %v", newPeer.String(), err)
			continue
		}

		peers = append(peers, ifiAddress.Overlay)
	}

	if s.addPeersHandler != nil {
		if err := s.addPeersHandler(ctx, peers...); err != nil {
			return err
		}
	}

	return nil
}
