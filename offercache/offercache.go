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
	mut                    sync.RWMutex
	offerSet               map[string]*mesos.Offer
	offerQueue             chan *mesos.Offer
	maxOffers              int
	singleInstancePerSlave bool
}

func New(maxOffers int, singleInstancePerSlave bool) *OfferCache {
	return &OfferCache{
		offerSet:               map[string]*mesos.Offer{},
		offerQueue:             make(chan *mesos.Offer, maxOffers),
		maxOffers:              maxOffers,
		singleInstancePerSlave: singleInstancePerSlave,
	}
}

func (oc *OfferCache) Push(newOffer *mesos.Offer) bool {
	oc.mut.Lock()
	defer oc.mut.Unlock()
	if len(oc.offerSet) < oc.maxOffers {
		// Reject offers from existing slaves.
		_, exists := oc.offerSet[newOffer.SlaveId.GetValue()]
		if exists && oc.singleInstancePerSlave {
			log.Info("Offer already exists for slave ", newOffer.SlaveId.GetValue())
			return false
		} else {
			oc.offerSet[newOffer.GetId().GetValue()] = newOffer
		}

		// Try to add offer to the queue, clearing out invalid
		// offers in order to make room if necessary.
		for i := 0; i < 2; i++ {
			select {
			case oc.offerQueue <- newOffer:
				return true
			default:
				oc.gc()
			}
		}
	}
	log.Info("We already have enough offers cached.")
	return false
}

func (oc *OfferCache) Rescind(offerId *mesos.OfferID) {
	oc.mut.Lock()
	defer oc.mut.Unlock()
	delete(oc.offerSet, offerId.GetValue())
}

func (oc *OfferCache) BlockingPop() *mesos.Offer {
	for offer := range oc.offerQueue {
		oc.mut.Lock()
		if _, ok := oc.offerSet[offer.GetId().GetValue()]; ok {
			delete(oc.offerSet, offer.GetId().GetValue())
			oc.mut.Unlock()
			return offer
		}
		oc.mut.Unlock()
	}
	// offerQueue was closed... this is unexpected.
	log.Error("offerQueue was closed unexpectedly.")
	return nil
}

func (oc *OfferCache) Len() int {
	oc.mut.RLock()
	defer oc.mut.RUnlock()
	return len(oc.offerSet)
}

// Not thread safe!  It is expected that any callers of this
// will handle their own synchronization.
func (oc *OfferCache) gc() {
	for i := 0; i < len(oc.offerSet)+1; i++ {
		select {
		case offer := <-oc.offerQueue:
			if _, ok := oc.offerSet[offer.GetId().GetValue()]; ok {
				// Requeue if this is still a valid offer.
				oc.offerQueue <- offer
			}
		default:
			// Nothing left to GC.
			return
		}
	}
}
