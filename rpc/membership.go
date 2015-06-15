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
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/mesosphere/etcd-mesos/common"

	log "github.com/golang/glog"
)

type ClusterMemberList struct {
	Members []struct {
		Id         string   `json:"id"`
		Name       string   `json:"name"`
		PeerURLS   []string `json:"peerURLS"`
		ClientURLS []string `json:"clientURLS"`
	} `json:"members"`
}

func ConfigureInstance(running map[string]*common.EtcdConfig, task string) {
	if len(running) == 0 {
		log.Info("No running members to configure.  Skipping.")
		return
	}
	// TODO(tyler) retry with exponential backoff
	// TODO(tyler) enforce invariant that all existing nodes must be healthy before adding a new one!
	err := HealthCheck(running)
	if err != nil {
		log.Errorf("!!!! cluster failed health check: %+v", err)
		// TODO refine this - I think it currently implicitly causes the task to finish when it tries to initialize
		return
	}

	newInstance, present := running[task]
	if !present {
		log.Errorf("task is not present in running map: %s", task)
		// TODO refine this - I think it currently implicitly causes the task to finish when it tries to initialize
		return
	}
	log.Infof("trying to reconfigure cluster for newInstance %+v", newInstance)
	for retries := 0; retries < 5; retries++ {
		for id, args := range running {
			if id == task {
				continue
			}
			url := fmt.Sprintf(
				"http://%s:%d/v2/members",
				args.Host,
				args.ClientPort)
			data := fmt.Sprintf(
				`{"peerURLs": ["http://%s:%d"]}`,
				newInstance.Host,
				newInstance.RpcPort)

			req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(data)))
			req.Header.Set("Content-Type", "application/json")

			client := &http.Client{
				Timeout: time.Second * 5,
			}
			resp, err := client.Do(req)
			if err != nil {
				log.Error(err)
				continue
			}
			defer resp.Body.Close()

			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				log.Errorf("Problem configuring instance: %s", err)
				continue
			}
			var memberList ClusterMemberList
			err = json.Unmarshal(body, &memberList)
			if err != nil {
				log.Errorf("Received unexpected response: %s", string(body))
				log.Errorf("Failed to unmarshal json: %s", err)
				continue
			}
			if len(memberList.Members) == 0 {
				err = errors.New("Remote node returned an empty etcd member list.")
				continue
			}
			log.Infof("Successfully configured new node: %+v\n", memberList)
			return

			// TODO(tyler) check response, and return if it's valid
			// TODO(tyler) invariant: member list should now contain node
		}
	}
}

func MemberList(running map[string]*common.EtcdConfig) (nameToIdent map[string]string, err error) {
	// TODO(tyler) retry with exponential backoff
	nameToIdent = map[string]string{}

	for _, args := range running {
		url := fmt.Sprintf(
			"http://%s:%d/v2/members",
			args.Host,
			args.ClientPort)

		resp, err := http.Get(url)
		if err != nil {
			log.Error("Could not query %s for member list: %+v", args.Host, err)
			continue
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Error("could not query %s for member list", args.Host)
			continue
		}
		log.Info("MemberList response:", string(body))
		var memberList ClusterMemberList
		err = json.Unmarshal(body, &memberList)
		if err != nil {
			log.Error(err)
			continue
		}
		if len(memberList.Members) == 0 {
			err = errors.New("Remote node returned an empty etcd member list.")
			continue
		}
		log.Infof("got member list: %+v\n", memberList)

		for _, m := range memberList.Members {
			nameToIdent[m.Name] = m.Id
		}
		break
	}
	return nameToIdent, err
}

func RemoveInstance(running map[string]*common.EtcdConfig, task string) {
	// TODO(tyler) retry with exponential backoff
	log.Infof("Attempting to remove task %s from "+
		"the etcd cluster configuration.", task)
	members, err := MemberList(running)
	if err != nil {
		// TODO(tyler) handle
	}
	ident := members[task]
	for id, args := range running {
		if id == task {
			continue
		}
		url := fmt.Sprintf(
			"http://%s:%d/v2/members/%s",
			args.Host,
			args.ClientPort,
			ident)

		req, err := http.NewRequest("DELETE", url, nil)

		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			log.Error(err)
			continue
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Errorf("Problem removing instance for this attempt: %s", err)
			continue
		}
		log.Info("RemoveInstance response: ", string(body))
		// TODO(tyler) check response, and return if it's valid
		// TODO(tyler) invariant: member list should no longer contain node
	}
}
