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
	"strings"
	"syscall"

	"github.com/mesosphere/etcd-mesos/rpc"
)

func main() {
	master :=
		flag.String("master", "127.0.0.1:5050", "Master address <ip:port>, "+
			"or zk://host:port,host:port/mesos uri")
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
		result, err := rpc.GetMasterFromZK(*master)
		if err != nil {
			log.Fatal(err)
		}
		*master = result
	}

	// Pull the current tasks from the mesos master
	log.Printf("Pulling state.json from master: %s\n", *master)
	peers, err := rpc.GetPeersFromMaster(*master, *clusterName)
	if err != nil {
		log.Fatal(err)
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
	log.Fatal(err)
}
