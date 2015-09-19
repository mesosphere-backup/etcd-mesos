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
	"bytes"
	"flag"
	"io/ioutil"
	"log"
	"strings"
	"syscall"
	"text/template"

	"github.com/mesosphere/etcd-mesos/rpc"
)

type proxyArgs struct {
	Bin            string
	InitialCluster string
	DataDir        string
	ListenAddrs    string
	AdvertiseAddrs string
}

var cmdTemplate = template.Must(template.New("etcd-cmd").Parse(
	`{{.Bin}} --proxy=on ` +
		`--data-dir={{.DataDir}} ` +
		`--listen-client-urls={{.ListenAddrs}} ` +
		`--advertise-client-urls={{.AdvertiseAddrs}} ` +
		`--initial-cluster={{.InitialCluster}}`,
))

func main() {
	master :=
		flag.String("master", "127.0.0.1:5050", "Master address <ip:port>, "+
			"or zk://host:port,host:port/mesos uri for discovering the current "+
			"mesos master.")
	etcdBin :=
		flag.String("etcd-bin", "./bin/etcd", "Path to etcd binary to "+
			"configure and run.")
	clusterName :=
		flag.String("cluster-name", "default", "Unique name of the etcd cluster "+
			"to connect to, corresponding to the --cluster-name arg passed to the "+
			"etcd-mesos-scheduler")
	frameworkName :=
		flag.String("framework-name", "", "Mesos framework name for this etcd cluster")
	flag.String("data-dir", "default.etcd", "Path to the data directory.")
	clientUrls :=
		flag.String("listen-client-urls", "http://localhost:2379,http://localhost:4001",
			"List of URLs to listen on for client traffic.")
	flag.Parse()

	if *frameworkName == "" {
		*frameworkName = "etcd-" + *clusterName
	}

	// Generate a temporary directory for the proxy
	path, err := ioutil.TempDir("", "etcd-mesos-proxy")
	if err != nil {
		log.Fatal(err)
	}

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
	state, err := rpc.GetState("http://" + *master)
	if err != nil {
		log.Fatal(err)
	}
	peers, err := rpc.GetPeersFromState(state, *frameworkName)
	if err != nil {
		log.Fatal(err)
	}

	// Format etcd proxy configuration options
	var args bytes.Buffer
	err = cmdTemplate.Execute(&args, proxyArgs{
		Bin:            *etcdBin,
		InitialCluster: strings.Join(peers, ","),
		DataDir:        path,
		ListenAddrs:    *clientUrls,
		AdvertiseAddrs: *clientUrls,
	})
	if err != nil {
		log.Fatal(err)
	}

	err = syscall.Exec(*etcdBin, strings.Fields(args.String()), []string{})
	log.Fatal(err)
}
