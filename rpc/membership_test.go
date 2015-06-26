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

package rpc

import (
	"reflect"
	gotesting "testing"

	"github.com/coreos/etcd/etcdserver/etcdhttp/httptypes"
	"github.com/stretchr/testify/assert"

	"github.com/mesosphere/etcd-mesos/config"
	emtesting "github.com/mesosphere/etcd-mesos/testing"
)

func TestConfigureInstance(t *gotesting.T) {
}

func TestMemberList(t *gotesting.T) {
	memberList := config.ClusterMemberList{
		Members: []httptypes.Member{
			httptypes.Member{
				ID:         "1",
				Name:       "etcd-1",
				PeerURLs:   nil,
				ClientURLs: nil,
			},
			httptypes.Member{
				ID:         "2",
				Name:       "etcd-2",
				PeerURLs:   nil,
				ClientURLs: nil,
			},
			httptypes.Member{
				ID:         "3",
				Name:       "etcd-3",
				PeerURLs:   nil,
				ClientURLs: nil,
			},
		},
	}

	_, port1, err := emtesting.NewTestEtcdServer(t, memberList)
	if err != nil {
		t.Fatalf("Failed to create test etcd server: %s", err)
	}
	_, port2, err := emtesting.NewTestEtcdServer(t, memberList)
	if err != nil {
		t.Fatalf("Failed to create test etcd server: %s", err)
	}
	_, port3, err := emtesting.NewTestEtcdServer(t, memberList)
	if err != nil {
		t.Fatalf("Failed to create test etcd server: %s", err)
	}
	running := map[string]*config.Etcd{
		"1": {
			Name:       "etcd-1",
			Host:       "localhost",
			ClientPort: uint64(port1),
		},
		"2": {
			Name:       "etcd-2",
			Host:       "localhost",
			ClientPort: uint64(port2),
		},
		"3": {
			Name:       "etcd-3",
			Host:       "localhost",
			ClientPort: uint64(port3),
		},
	}

	nameToIdent, err := MemberList(running)

	assert.Equal(
		t,
		reflect.DeepEqual(
			nameToIdent,
			map[string]string{
				"etcd-1": "1",
				"etcd-2": "2",
				"etcd-3": "3",
			},
		),
		true,
		"MemberList should return the running instances.")
}

func TestRemoveInstance(t *gotesting.T) {
}
