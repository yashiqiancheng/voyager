// Copyright 2020 The Smart Chain Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package debugapi

import (
	"errors"
	"math/big"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yanhuangpai/voyager/pkg/infinity"
	"github.com/yanhuangpai/voyager/pkg/jsonhttp"
	"github.com/yanhuangpai/voyager/pkg/settlement"
)

var (
	errCantSettlements     = "can not get settlements"
	errCantSettlementsPeer = "can not get settlements for peer"
)

type settlementResponse struct {
	Peer               string   `json:"peer"`
	SettlementReceived *big.Int `json:"received"`
	SettlementSent     *big.Int `json:"sent"`
}

type settlementsResponse struct {
	TotalSettlementReceived *big.Int             `json:"totalreceived"`
	TotalSettlementSent     *big.Int             `json:"totalsent"`
	Settlements             []settlementResponse `json:"settlements"`
}

func (s *Service) settlementsHandler(w http.ResponseWriter, r *http.Request) {

	settlementsSent, err := s.settlement.SettlementsSent()
	if err != nil {
		jsonhttp.InternalServerError(w, errCantSettlements)
		s.logger.Debugf("debug api: sent settlements: %v", err)
		s.logger.Error("debug api: can not get sent settlements")
		return
	}
	settlementsReceived, err := s.settlement.SettlementsReceived()
	if err != nil {
		jsonhttp.InternalServerError(w, errCantSettlements)
		s.logger.Debugf("debug api: received settlements: %v", err)
		s.logger.Error("debug api: can not get received settlements")
		return
	}

	totalReceived := big.NewInt(0)
	totalSent := big.NewInt(0)

	settlementResponses := make(map[string]settlementResponse)

	for a, b := range settlementsSent {
		settlementResponses[a] = settlementResponse{
			Peer:               a,
			SettlementSent:     b,
			SettlementReceived: big.NewInt(0),
		}
		totalSent.Add(b, totalSent)
	}

	for a, b := range settlementsReceived {
		if _, ok := settlementResponses[a]; ok {
			t := settlementResponses[a]
			t.SettlementReceived = b
			settlementResponses[a] = t
		} else {
			settlementResponses[a] = settlementResponse{
				Peer:               a,
				SettlementSent:     big.NewInt(0),
				SettlementReceived: b,
			}
		}
		totalReceived.Add(b, totalReceived)
	}

	settlementResponsesArray := make([]settlementResponse, len(settlementResponses))
	i := 0
	for k := range settlementResponses {
		settlementResponsesArray[i] = settlementResponses[k]
		i++
	}

	jsonhttp.OK(w, settlementsResponse{TotalSettlementReceived: totalReceived, TotalSettlementSent: totalSent, Settlements: settlementResponsesArray})
}

func (s *Service) peerSettlementsHandler(w http.ResponseWriter, r *http.Request) {
	addr := mux.Vars(r)["peer"]
	peer, err := infinity.ParseHexAddress(addr)
	if err != nil {
		s.logger.Debugf("debug api: settlements peer: invalid peer address %s: %v", addr, err)
		s.logger.Errorf("debug api: settlements peer: invalid peer address %s", addr)
		jsonhttp.NotFound(w, errInvalidAddress)
		return
	}

	peerexists := false

	received, err := s.settlement.TotalReceived(peer)
	if err != nil {
		if !errors.Is(err, settlement.ErrPeerNoSettlements) {
			s.logger.Debugf("debug api: settlements peer: get peer %s received settlement: %v", peer.String(), err)
			s.logger.Errorf("debug api: settlements peer: can't get peer %s received settlement", peer.String())
			jsonhttp.InternalServerError(w, errCantSettlementsPeer)
			return
		} else {
			received = big.NewInt(0)
		}
	}

	if err == nil {
		peerexists = true
	}

	sent, err := s.settlement.TotalSent(peer)
	if err != nil {
		if !errors.Is(err, settlement.ErrPeerNoSettlements) {
			s.logger.Debugf("debug api: settlements peer: get peer %s sent settlement: %v", peer.String(), err)
			s.logger.Errorf("debug api: settlements peer: can't get peer %s sent settlement", peer.String())
			jsonhttp.InternalServerError(w, errCantSettlementsPeer)
			return
		} else {
			sent = big.NewInt(0)
		}
	}

	if err == nil {
		peerexists = true
	}

	if !peerexists {
		jsonhttp.NotFound(w, settlement.ErrPeerNoSettlements)
		return
	}

	jsonhttp.OK(w, settlementResponse{
		Peer:               peer.String(),
		SettlementReceived: received,
		SettlementSent:     sent,
	})
}
