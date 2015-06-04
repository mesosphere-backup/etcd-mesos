/**
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package offercache

import (
	"sync"

	log "github.com/golang/glog"
	mesos "github.com/mesos/mesos-go/mesosproto"
)

type OfferCache struct {
	sync.RWMutex
	offerSet   map[*mesos.Offer]struct{}
	offerQueue chan *mesos.Offer
	maxOffers  int
}

func NewOfferCache(maxOffers int) *OfferCache {
	return &OfferCache{
		offerSet:   map[*mesos.Offer]struct{}{},
		offerQueue: make(chan *mesos.Offer, maxOffers+2048),
		maxOffers:  maxOffers,
	}
}

func (oc *OfferCache) Push(newOffer *mesos.Offer) bool {
	oc.Lock()
	defer oc.Unlock()
	if len(oc.offerSet) < oc.maxOffers+1 {
		// Reject offers from existing slaves.
		for offer, _ := range oc.offerSet {
			if offer.SlaveId.GetValue() == newOffer.SlaveId.GetValue() {
				log.Info("Offer already exists for slave ", offer.SlaveId.GetValue())
				return false
			}
		}
		oc.offerSet[newOffer] = struct{}{}

		// Try to add offer to the queue, clearing out invalid
		// offers in order to make room if necessary.
		for {
			select {
			case oc.offerQueue <- newOffer:
				return true
			default:
				oc.Unlock()
				oc.Gc()
				oc.Lock()
			}
		}
	} else {
		log.Info("We already have enough offers cached.")
		return false
	}
}

func (oc *OfferCache) Rescind(offerId *mesos.OfferID) {
	oc.Lock()
	defer oc.Unlock()
	for offer, _ := range oc.offerSet {
		if offer.GetId() == offerId {
			delete(oc.offerSet, offer)
			return
		}
	}
}

func (oc *OfferCache) BlockingPop() *mesos.Offer {
	for {
		offer := <-oc.offerQueue
		if _, ok := oc.offerSet[offer]; ok {
			oc.Lock()
			defer oc.Unlock()
			delete(oc.offerSet, offer)
			return offer
		}
	}
}

func (oc *OfferCache) Len() int {
	oc.RLock()
	defer oc.RUnlock()
	return len(oc.offerSet)
}

func (oc *OfferCache) Gc() {
	oc.Lock()
	defer oc.Unlock()
	for i := 0; i < len(oc.offerSet)+1; i++ {
		select {
		case offer := <-oc.offerQueue:
			if _, ok := oc.offerSet[offer]; ok {
				// Requeue if this is still a valid offer.
				oc.offerQueue <- offer
			}
		default:
			// Nothing left to GC.
			return
		}
	}
}
