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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/mesosphere/etcd-mesos/config"
	"github.com/mesosphere/etcd-mesos/errors"

	etcdstats "github.com/coreos/etcd/etcdserver/stats"
	"github.com/coreos/go-etcd/etcd"
	log "github.com/golang/glog"
)

// HealthCheck performs basic sanity checks on an etcd cluster.
// This function explicitly forgoes backoffs.  If it fails
// something, it is assumed to be unhealthy.
func HealthCheck(running map[string]*config.Node) error {
	// TODO(tyler) invariant: all nodes have same leader
	if len(running) == 0 {
		return nil
	}
	var validEndpoint string
	for _, args := range running {
		url := fmt.Sprintf(
			"http://%s:%d",
			args.Host,
			args.ClientPort,
		)
		client := http.Client{
			Timeout: 5 * time.Second,
		}
		resp, err := client.Get(url + "/v2/stats/leader")
		if err != nil {
			log.Errorf("Could not query %s for leader stats: %+v", url, err)
			continue
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Errorf("Could not query %s for leader stats", url)
			continue
		}
		log.Info("Leader stats response:", string(body))
		ls := &etcdstats.LeaderStats{}
		err = json.Unmarshal(body, ls)
		if err != nil {
			log.Errorf("received invalid LeaderStats from endpoint %s:%s",
				url, string(body))
			continue
		}
		validEndpoint = url
		break
	}

	if validEndpoint == "" {
		log.Error("Leader could not be determined.")
		return errors.ErrNoLeader
	}

	client := etcd.NewClient([]string{validEndpoint})
	if ok := client.SyncCluster(); !ok {
		log.Error("Could not establish connection "+
			"with cluster using endpoints %+v", validEndpoint)
		return errors.ErrEtcdConnection
	}

	resp1, err := client.Get("/", false, false)
	if err != nil {
		log.Errorf("Could not query cluster: %s", err)
		return errors.ErrEtcdEndpoint
	}

	// Give the cluster some time to propagate AppendEntries.
	time.Sleep(time.Second)

	resp2, err := client.Get("/", false, false)
	if err != nil {
		log.Errorf("Could not query cluster: %s", err)
		return errors.ErrEtcdEndpoint
	}

	if resp1.RaftTerm != resp2.RaftTerm {
		log.Error("Raft terms has increased while monitoring for " +
			"1 second.  Leader is unstable.")
		return errors.ErrEtcdRaftTermInstability
	}

	if resp1.RaftIndex == resp2.RaftIndex {
		log.Error("Raft commit index has not increased while " +
			"monitoring for 1 second.  The cluster is not making progress.")
		return errors.ErrEtcdRaftStall
	}
	return nil
}
