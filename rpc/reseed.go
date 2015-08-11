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
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
	"time"

	"github.com/mesosphere/etcd-mesos/config"

	"github.com/coreos/go-etcd/etcd"
	log "github.com/golang/glog"
)

type nodeIndex struct {
	RaftIndex uint64
	Node      string
}

type nodeIndices []nodeIndex

func (n nodeIndices) Len() int {
	return len(n)
}

func (n nodeIndices) Less(i, j int) bool {
	return n[i].RaftIndex < n[j].RaftIndex
}
func (n nodeIndices) Swap(i, j int) {
	n[i], n[j] = n[j], n[i]
}

func RankReseedCandidates(running map[string]*config.Node) []nodeIndex {
	nodeIndices := nodeIndices{}

	for id, args := range running {
		url := fmt.Sprintf(
			"http://%s:%d",
			args.Host,
			args.ClientPort,
		)
		client := etcd.NewClient([]string{url})
		if ok := client.SyncCluster(); !ok {
			log.Error("Could not establish connection "+
				"with cluster using endpoints %+v", url)
			continue
		}

		resp, err := client.Get("/", false, false)
		if err != nil {
			log.Errorf("Could not query cluster: %s", err)
			continue
		}

		nodeIndices = append(nodeIndices, nodeIndex{
			RaftIndex: resp.RaftIndex,
			Node:      id,
		})
	}
	sort.Sort(nodeIndices)
	return nodeIndices
}

func TriggerReseed(node *config.Node) error {
	url := fmt.Sprintf(
		"http://%s:%d",
		node.Host,
		node.ReseedPort,
	)
	client := http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Get(url + "/reseed")
	if err != nil {
		log.Errorf("Could not request %s to reseed: %+v", url, err)
		return err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Errorf("Could not trigger reseed on %s: %v", url, err)
		return err
	}
	if string(body) != "ok" {
		err = fmt.Errorf("Could not trigger reseed on %s: %v", url, err)
		log.Error(err)
		return err
	}
	return nil
}
