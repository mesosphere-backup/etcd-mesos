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
	"testing"
	"time"
	//"testing/quick"

	mesos "github.com/mesos/mesos-go/mesosproto"
	util "github.com/mesos/mesos-go/mesosutil"
	"github.com/stretchr/testify/assert"
)

func TestPush(t *testing.T) {
	for i, tt := range []struct {
		offers []*mesos.Offer
		want   int
	}{
		{[]*mesos.Offer{newOffer("a", "a")}, 1},
		{[]*mesos.Offer{
			newOffer("a", "a"),
			newOffer("a", "a"),
		}, 1},
		{[]*mesos.Offer{
			newOffer("a", "a"),
			newOffer("b", "b"),
			newOffer("a", "a"),
		}, 2},
		{[]*mesos.Offer{
			newOffer("a", "a"),
			newOffer("b", "b"),
			newOffer("c", "c"),
			newOffer("d", "d"),
			newOffer("e", "e"),
			newOffer("f", "f"),
			newOffer("g", "g"),
		}, 5},
	} {
		oc := New(5, false)
		for _, o := range tt.offers {
			oc.Push(o)
		}
		if got := oc.Len(); got != tt.want {
			t.Errorf("test #%d: got : %s, want: %s", i, got, tt.want)
		}
	}
}

func TestRescind(t *testing.T) {
	for i, tt := range []struct {
		offers   []*mesos.Offer
		rescinds []string
		want     int
	}{
		{[]*mesos.Offer{newOffer("a", "a")}, []string{"a"}, 0},
		{[]*mesos.Offer{newOffer("a", "a")}, []string{"b"}, 1},
		{[]*mesos.Offer{}, []string{"a"}, 0},
		{[]*mesos.Offer{
			newOffer("a", "a"),
			newOffer("b", "b"),
			newOffer("c", "c"),
			newOffer("d", "d"),
			newOffer("e", "e"),
			newOffer("f", "f"),
			newOffer("g", "g"),
		}, []string{"a", "g"}, 4},
	} {
		oc := New(5, false)
		for _, o := range tt.offers {
			oc.Push(o)
		}
		for _, r := range tt.rescinds {
			oc.Rescind(util.NewOfferID(r))
		}
		if got := oc.Len(); got != tt.want {
			t.Errorf("test #%d: got : %s, want: %s", i, got, tt.want)
		}
	}

}

func TestBlockingPop(t *testing.T) {
	for i, tt := range []struct {
		offers   []*mesos.Offer
		rescinds []string
		want     int
	}{
		{[]*mesos.Offer{newOffer("a", "a")}, []string{"b"}, 1},
		{[]*mesos.Offer{
			newOffer("a", "a"),
			newOffer("b", "b"),
			newOffer("c", "c"),
			newOffer("d", "d"),
			newOffer("e", "e"),
			newOffer("f", "f"),
			newOffer("g", "g"),
		}, []string{"a", "g"}, 4},
	} {
		oc := New(5, false)
		for _, o := range tt.offers {
			oc.Push(o)
		}
		for _, r := range tt.rescinds {
			oc.Rescind(util.NewOfferID(r))
		}

		got := func() int {
			n := 0
			for oc.Len() > 0 {
				c := make(chan struct{})
				go func() {
					oc.BlockingPop()
					c <- struct{}{}
				}()
				select {
				case <-c:
					n += 1
				case <-time.After(time.Second):
					return n
				}
			}
			return n
		}()

		if got != tt.want {
			t.Errorf("test #%d: got : %s, want: %s", i, got, tt.want)
		}
	}
}

func Test_gc(t *testing.T) {
	oc := New(5, false)
	for i := 0; i < 5000; i++ {
		oc.Rescind(util.NewOfferID(string(i - 50)))
		oc.Push(newOffer(string(i), string(i)))
	}
	assert.Equal(t, 5, oc.Len())
}

func newOffer(offer, slave string) *mesos.Offer {
	return &mesos.Offer{
		Id:      util.NewOfferID(offer),
		SlaveId: util.NewSlaveID(slave),
	}
}
