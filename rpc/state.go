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
	"errors"
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"time"

	log "github.com/golang/glog"

	"github.com/mesosphere/etcd-mesos/config"
)

type Task struct {
	ExecutorID  string `json:"executor_id"`
	FrameworkID string `json:"framework_id"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Resources   struct {
		Cpus  float64 `json:"cpus"`
		Disk  float64 `json:"disk"`
		Mem   float64 `json:"mem"`
		Ports string  `json:"ports"`
	} `json:"resources"`
	SlaveID  string `json:"slave_id"`
	State    string `json:"state"`
	Statuses []struct {
		State     string  `json:"state"`
		Timestamp float64 `json:"timestamp"`
	} `json:"statuses"`
}

type Framework struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Tasks []Task `json:"tasks"`
}

// This is only a partial section of the returned JSON.
// In the future we may need to add more fields if they
// have a reason to be queried.  Hitting state.json is
// an antipattern, but we only do it during framework
// initialization.
type MasterState struct {
	Frameworks []Framework `json:"frameworks"`
}

func GetState(master string) (*MasterState, error) {
	backoff := 1
	log.Infof("Trying to get master state from %s/state.json", master)
	var outerErr error
	masterState := &MasterState{}
	for retries := 0; retries < RPC_RETRIES; retries++ {
		for {
			client := http.Client{
				Timeout: RPC_TIMEOUT,
			}
			resp, err := client.Get(fmt.Sprintf("%s/state.json", master))
			if err != nil {
				outerErr = err
				break
			}
			blob, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				outerErr = err
				break
			}

			err = json.Unmarshal(blob, masterState)
			if err == nil {
				return masterState, nil
			}
			log.Error(err)
		}
		log.Warningf("Failed to get state.json: %v", outerErr)
		time.Sleep(time.Duration(backoff) * time.Second)
		backoff = int(math.Min(float64(backoff<<1), 8))
	}
	return nil, outerErr
}

func GetPeersFromState(state *MasterState, clusterName string) ([]string, error) {
	var framework *Framework
	for _, f := range state.Frameworks {
		if f.Name == "etcd-"+clusterName {
			framework = &f
			break
		}
	}
	if framework == nil {
		return []string{}, errors.New("Could not find etcd-" + clusterName +
			" in the mesos master's state.json")
	}

	peers := []string{}
	for _, t := range framework.Tasks {
		if t.State == "TASK_RUNNING" {
			node, err := config.Parse(t.ID)
			if err != nil {
				return []string{}, err
			}
			peers = append(peers, fmt.Sprintf("%s=http://%s:%d",
				node.Name, node.Host, node.RPCPort))
		}
	}
	return peers, nil
}
