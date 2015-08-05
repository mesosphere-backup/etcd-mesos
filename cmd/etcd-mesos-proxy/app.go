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

package main

import (
	"flag"
	"fmt"
	"log"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/mesosphere/etcd-mesos/rpc"
	"github.com/samuel/go-zookeeper/zk"
)

func main() {
	master :=
		flag.String("master", "127.0.0.1:5050", "Master address <ip:port>")
	etcdBin :=
		flag.String("etcd-bin", "./bin/etcd", "Path to etcd binary.")
	clusterName :=
		flag.String("cluster-name", "default", "Unique name of the etcd cluster to connect to.")
	dataDir :=
		flag.String("data-dir", "default.etcd", "Path to the data directory.")
	clientUrls :=
		flag.String("listen-client-urls", "http://localhost:2379,http://localhost:4001",
			"List of URLs to listen on for client traffic.")
	flag.Parse()

	// Pull current master from ZK if a ZK URI was provided
	if strings.HasPrefix(*master, "zk://") {
		log.Printf("Trying to connect to zk cluster %s", *master)
		servers, chroot, err := rpc.ParseZKURI(*master)
		c, _, err := zk.Connect(servers, time.Second*5)
		if err != nil {
			log.Fatal(err)
		}

		children, _, err := c.Children(chroot)
		if err != nil {
			log.Fatal(err)
		}

		var lowest *string
		ss := sort.StringSlice(children)
		ss.Sort()
		for i := 0; i < len(ss); i++ {
			if strings.HasPrefix(ss[i], "info_") {
				lowest = &ss[i]
				break
			}
		}
		if lowest == nil {
			log.Fatal("Could not find current mesos master in zk")
		}
		rawData, _, err := c.Get(chroot + "/" + *lowest)
		c.Close()
		mraw := strings.Split(string(rawData), "master@")[1]
		master = &strings.Split(mraw, "*")[0]
	}

	// Pull the current tasks from the mesos master
	log.Printf("Pulling state.json from master: %s\n", *master)
	state, err := rpc.GetState("http://" + *master)
	if err != nil {
		log.Fatal(err)
	}

	var framework *rpc.Framework
	for _, f := range state.Frameworks {
		if f.Name == "etcd-"+*clusterName {
			framework = &f
		}
	}
	if framework == nil {
		log.Fatalf("Could not find etcd-%s in the mesos master's state.json",
			*clusterName)
	}

	peers := []string{}
	for _, t := range framework.Tasks {
		if t.State == "TASK_RUNNING" {
			splits := strings.Split(t.ID, " ")
			peers = append(peers, fmt.Sprintf("%s=http://%s:%s",
				splits[0], splits[1], splits[2]))
		}
	}

	// Format etcd proxy configuration options
	initialCluster := fmt.Sprintf("--initial-cluster=%s", strings.Join(peers, ","))
	dataArg := fmt.Sprintf("--data-dir=%s", *dataDir)
	listenArg := fmt.Sprintf("--listen-client-urls=%s", *clientUrls)
	advertiseArg := fmt.Sprintf("--advertise-client-urls=%s", *clientUrls)

	err = syscall.Exec(*etcdBin, []string{
		"--proxy=on",
		initialCluster,
		dataArg,
		listenArg,
		advertiseArg,
	}, []string{})
}
