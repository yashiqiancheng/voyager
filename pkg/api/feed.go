// Copyright 2021 The Smart Chain Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package api

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/mux"
	"github.com/yanhuangpai/voyager/pkg/feeds"
	"github.com/yanhuangpai/voyager/pkg/file/loadsave"
	"github.com/yanhuangpai/voyager/pkg/infinity"
	"github.com/yanhuangpai/voyager/pkg/jsonhttp"
	"github.com/yanhuangpai/voyager/pkg/manifest"
	"github.com/yanhuangpai/voyager/pkg/soc"
)

const (
	feedMetadataEntryOwner = "infinity-feed-owner"
	feedMetadataEntryTopic = "infinity-feed-topic"
	feedMetadataEntryType  = "infinity-feed-type"
)

var errInvalidFeedUpdate = errors.New("invalid feed update")

type feedReferenceResponse struct {
	Reference infinity.Address `json:"reference"`
}

func (s *server) feedGetHandler(w http.ResponseWriter, r *http.Request) {
	owner, err := hex.DecodeString(mux.Vars(r)["owner"])
	if err != nil {
		s.logger.Debugf("feed get: decode owner: %v", err)
		s.logger.Error("feed get: bad owner")
		jsonhttp.BadRequest(w, "bad owner")
		return
	}

	topic, err := hex.DecodeString(mux.Vars(r)["topic"])
	if err != nil {
		s.logger.Debugf("feed get: decode topic: %v", err)
		s.logger.Error("feed get: bad topic")
		jsonhttp.BadRequest(w, "bad topic")
		return
	}

	var at int64
	atStr := r.URL.Query().Get("at")
	if atStr != "" {
		at, err = strconv.ParseInt(atStr, 10, 64)
		if err != nil {
			s.logger.Debugf("feed get: decode at: %v", err)
			s.logger.Error("feed get: bad at")
			jsonhttp.BadRequest(w, "bad at")
			return
		}
	} else {
		at = time.Now().Unix()
	}

	f := feeds.New(topic, common.BytesToAddress(owner))
	lookup, err := s.feedFactory.NewLookup(feeds.Sequence, f)
	if err != nil {
		s.logger.Debugf("feed get: new lookup: %v", err)
		s.logger.Error("feed get: new lookup")
		jsonhttp.InternalServerError(w, "new lookup")
		return
	}

	ch, cur, next, err := lookup.At(r.Context(), at, 0)
	if err != nil {
		s.logger.Debugf("feed get: lookup: %v", err)
		s.logger.Error("feed get: lookup error")
		jsonhttp.NotFound(w, "lookup failed")
		return
	}

	// KLUDGE: if a feed was never updated, the chunk will be nil
	if ch == nil {
		s.logger.Debugf("feed get: no update found: %v", err)
		s.logger.Error("feed get: no update found")
		jsonhttp.NotFound(w, "lookup failed")
		return
	}

	ref, _, err := parseFeedUpdate(ch)
	if err != nil {
		s.logger.Debugf("feed get: parse update: %v", err)
		s.logger.Error("feed get: parse update")
		jsonhttp.InternalServerError(w, "parse update")
		return
	}

	curBytes, err := cur.MarshalBinary()
	if err != nil {
		s.logger.Debugf("feed get: marshal current index: %v", err)
		s.logger.Error("feed get: marshal index")
		jsonhttp.InternalServerError(w, "marshal index")
		return
	}

	nextBytes, err := next.MarshalBinary()
	if err != nil {
		s.logger.Debugf("feed get: marshal next index: %v", err)
		s.logger.Error("feed get: marshal index")
		jsonhttp.InternalServerError(w, "marshal index")
		return
	}

	w.Header().Set(InfinityFeedIndexHeader, hex.EncodeToString(curBytes))
	w.Header().Set(InfinityFeedIndexNextHeader, hex.EncodeToString(nextBytes))
	w.Header().Set("Access-Control-Expose-Headers", fmt.Sprintf("%s, %s", InfinityFeedIndexHeader, InfinityFeedIndexNextHeader))

	jsonhttp.OK(w, feedReferenceResponse{Reference: ref})
}

func (s *server) feedPostHandler(w http.ResponseWriter, r *http.Request) {
	owner, err := hex.DecodeString(mux.Vars(r)["owner"])
	if err != nil {
		s.logger.Debugf("feed put: decode owner: %v", err)
		s.logger.Error("feed put: bad owner")
		jsonhttp.BadRequest(w, "bad owner")
		return
	}

	topic, err := hex.DecodeString(mux.Vars(r)["topic"])
	if err != nil {
		s.logger.Debugf("feed put: decode topic: %v", err)
		s.logger.Error("feed put: bad topic")
		jsonhttp.BadRequest(w, "bad topic")
		return
	}
	l := loadsave.New(s.storer, requestModePut(r), false)
	feedManifest, err := manifest.NewDefaultManifest(l, false)
	if err != nil {
		s.logger.Debugf("feed put: new manifest: %v", err)
		s.logger.Error("feed put: new manifest")
		jsonhttp.InternalServerError(w, "create manifest")
		return
	}

	meta := map[string]string{
		feedMetadataEntryOwner: hex.EncodeToString(owner),
		feedMetadataEntryTopic: hex.EncodeToString(topic),
		feedMetadataEntryType:  feeds.Sequence.String(), // only sequence allowed for now
	}

	emptyAddr := make([]byte, 32)

	// a feed manifest stores the metadata at the root "/" path
	err = feedManifest.Add(r.Context(), "/", manifest.NewEntry(infinity.NewAddress(emptyAddr), meta))
	if err != nil {
		s.logger.Debugf("feed post: add manifest entry: %v", err)
		s.logger.Error("feed post: add manifest entry")
		jsonhttp.InternalServerError(w, nil)
		return
	}
	ref, err := feedManifest.Store(r.Context())
	if err != nil {
		s.logger.Debugf("feed post: store manifest: %v", err)
		s.logger.Error("feed post: store manifest")
		jsonhttp.InternalServerError(w, nil)
		return
	}
	jsonhttp.Created(w, feedReferenceResponse{Reference: ref})
}

func parseFeedUpdate(ch infinity.Chunk) (infinity.Address, int64, error) {
	s, err := soc.FromChunk(ch)
	if err != nil {
		return infinity.ZeroAddress, 0, fmt.Errorf("soc unmarshal: %w", err)
	}

	update := s.WrappedChunk().Data()
	// split the timestamp and reference
	// possible values right now:
	// unencrypted ref: span+timestamp+ref => 8+8+32=48
	// encrypted ref: span+timestamp+ref+decryptKey => 8+8+64=80
	if len(update) != 48 && len(update) != 80 {
		return infinity.ZeroAddress, 0, errInvalidFeedUpdate
	}
	ts := binary.BigEndian.Uint64(update[8:16])
	ref := infinity.NewAddress(update[16:])
	return ref, int64(ts), nil
}
