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
	"io/ioutil"
	"net"
	"net/http"

	"github.com/gogo/protobuf/proto"
	log "github.com/golang/glog"
	"github.com/mesos/mesos-go/auth"
	"github.com/mesos/mesos-go/auth/sasl"
	"github.com/mesos/mesos-go/auth/sasl/mech"
	mesos "github.com/mesos/mesos-go/mesosproto"
	"github.com/mesos/mesos-go/scheduler"
	"github.com/samuel/go-zookeeper/zk"
	"golang.org/x/net/context"

	"github.com/mesosphere/etcd-mesos/rpc"
	etcdscheduler "github.com/mesosphere/etcd-mesos/scheduler"
)

func parseIP(address string) net.IP {
	addr, err := net.LookupIP(address)
	if err != nil {
		log.Fatal(err)
	}
	if len(addr) < 1 {
		log.Fatalf("failed to parse IP from address '%v'", address)
	}
	return addr[0]
}

func main() {
	singleInstancePerSlave :=
		flag.Bool("single-instance-per-slave", true, "Only allow one etcd instance to be started per slave")
	taskCount :=
		flag.Int("task-count", 5, "Total task count to run")
	artifactPort :=
		flag.Int("artifactPort", 12300, "Binding port for artifact server")
	failoverTimeoutSeconds :=
		flag.Float64("failover-timeout-seconds", 60*60*24*7, "Mesos framework failover timeout in seconds")
	executorPath :=
		flag.String("executor", "./bin/etcd_executor", "Path to test executor")
	etcdPath :=
		flag.String("etcd", "./bin/etcd", "Path to test executor")
	address :=
		flag.String("address", "127.0.0.1", "Binding address for artifact server")
	restorePath :=
		flag.String("restore", "", "Local path or URI for an etcd backup to restore as a new cluster")
	master :=
		flag.String("master", "127.0.0.1:5050", "Master address <ip:port>")
	clusterName :=
		flag.String("clusterName", "default", "Unique name of this etcd cluster")
	mesosAuthPrincipal :=
		flag.String("mesos_authentication_principal", "", "Mesos authentication principal")
	mesosAuthSecretFile :=
		flag.String("mesos_authentication_secret_file", "", "Mesos authentication secret file")
	zkConnect :=
		flag.String("zk-framework-persist", "", "Zookeeper URI of the form zk://host1:port1,host2:port2/chroot/path")
	authProvider :=
		flag.String("mesos_authentication_provider", sasl.ProviderName,
			fmt.Sprintf("Authentication provider to use, default is SASL that supports mechanisms: %+v", mech.ListSupported()))
	flag.Parse()

	executorUris := []*mesos.CommandInfo_URI{}
	execUri := etcdscheduler.ServeExecutorArtifact(*executorPath, *address, *artifactPort)
	executorUris = append(executorUris, &mesos.CommandInfo_URI{
		Value:      execUri,
		Executable: proto.Bool(true),
	})
	etcdUri := etcdscheduler.ServeExecutorArtifact(*etcdPath, *address, *artifactPort)
	executorUris = append(executorUris, &mesos.CommandInfo_URI{
		Value:      etcdUri,
		Executable: proto.Bool(true),
	})

	go http.ListenAndServe(fmt.Sprintf("%s:%d", *address, *artifactPort), nil)
	log.V(2).Info("Serving executor artifacts...")

	bindingAddress := parseIP(*address)

	// chillFactor is the number of seconds that are slept for to allow for
	// convergence across the cluster during mutations.
	chillFactor := 10
	etcdScheduler := etcdscheduler.NewEtcdScheduler(
		*taskCount,
		chillFactor,
		executorUris,
		*singleInstancePerSlave,
	)
	etcdScheduler.ExecutorPath = *executorPath
	etcdScheduler.RestorePath = *restorePath
	etcdScheduler.Master = *master
	etcdScheduler.ClusterName = *clusterName
	etcdScheduler.ZkConnect = *zkConnect

	fwinfo := &mesos.FrameworkInfo{
		User:            proto.String(""), // Mesos-go will fill in user.
		Name:            proto.String("etcd: " + etcdScheduler.ClusterName),
		FailoverTimeout: proto.Float64(*failoverTimeoutSeconds),
	}

	cred := (*mesos.Credential)(nil)
	if *mesosAuthPrincipal != "" {
		fwinfo.Principal = proto.String(*mesosAuthPrincipal)
		secret, err := ioutil.ReadFile(*mesosAuthSecretFile)
		if err != nil {
			log.Fatal(err)
		}
		cred = &mesos.Credential{
			Principal: proto.String(*mesosAuthPrincipal),
			Secret:    secret,
		}
	}

	zkServers, zkChroot, err := rpc.ParseZKURI(*zkConnect)
	etcdScheduler.ZkServers = zkServers
	etcdScheduler.ZkChroot = zkChroot
	if err != nil && *zkConnect != "" {
		log.Fatalf("Error parsing zookeeper URI of %s: %s", *zkConnect, err)
	} else if *zkConnect != "" {
		previous, err := rpc.GetPreviousFrameworkID(
			zkServers,
			zkChroot,
			etcdScheduler.ClusterName,
		)
		if err != nil && err != zk.ErrNoNode {
			log.Fatalf("Could not retrieve previous framework ID: %s", err)
		} else if err == zk.ErrNoNode {
			log.Info("No previous persisted framework ID exists in zookeeper.")
		} else {
			log.Infof("Found stored framework ID in Zookeeper, "+
				"attempting to re-use: %s", previous)
			fwinfo.Id = &mesos.FrameworkID{
				Value: proto.String(previous),
			}
		}
	}

	config := scheduler.DriverConfig{
		Scheduler:      etcdScheduler,
		Framework:      fwinfo,
		Master:         etcdScheduler.Master,
		Credential:     cred,
		BindingAddress: bindingAddress,
		WithAuthContext: func(ctx context.Context) context.Context {
			ctx = auth.WithLoginProvider(ctx, *authProvider)
			ctx = sasl.WithBindingAddress(ctx, bindingAddress)
			return ctx
		},
	}

	driver, err := scheduler.NewMesosSchedulerDriver(config)

	if err != nil {
		log.Errorln("Unable to create a SchedulerDriver ", err.Error())
	}

	go etcdScheduler.SerialLauncher(driver)
	go etcdScheduler.PeriodicLaunchRequestor()
	go etcdScheduler.PeriodicPruner()

	if stat, err := driver.Run(); err != nil {
		log.Infof("Framework stopped with status %s and error: %s",
			stat.String(),
			err.Error())
	}
}
