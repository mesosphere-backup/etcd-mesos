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

package testing

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/mesosphere/etcd-mesos/config"
)

type TestEtcdServer struct {
	server *httptest.Server
}

func NewTestEtcdServer(t *testing.T, memberList config.ClusterMemberList) (*TestEtcdServer, int64, error) {
	ts := TestEtcdServer{}
	mux := http.NewServeMux()

	mux.HandleFunc("/v2/members", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		serializedMemberList, err := json.Marshal(&memberList)
		if err == nil {
			w.Write(serializedMemberList)
		} else {
			t.Fatal("Could not serialize test ClusterMemberList.")
		}
	})

	ts.server = httptest.NewServer(mux)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}

	go func() {
		err = http.Serve(listener, mux)
		if err != nil {
			t.Fatalf("Test Etcd server failed! %s", err)
		}
	}()

	parts := strings.Split(listener.Addr().String(), ":")
	if len(parts) != 2 {
		return nil, 0, errors.New("Bad address: " + listener.Addr().String())
	}
	port, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, 0, errors.New("Could not parse port into an int: " + parts[1])
	}

	return &ts, port, nil
}
