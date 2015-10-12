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
	"time"

	log "github.com/golang/glog"
	"github.com/mesos/mesos-go/executor"

	etcdexecutor "github.com/mesosphere/etcd-mesos/executor"
)

func main() {
	launchTimeout :=
		flag.Uint("launch-timeout", 240,
			"Seconds to retry launching an etcd instance for before giving up. "+
				"This should be long enough for a port occupied by a killed process "+
				"to be vacated.")
	flag.Parse()
	log.Infoln("Starting etcd Executor")

	dconfig := executor.DriverConfig{
		Executor: etcdexecutor.New(
			time.Duration(*launchTimeout) * time.Second,
		),
	}
	driver, err := executor.NewMesosExecutorDriver(dconfig)

	if err != nil {
		log.Infoln("Unable to create an ExecutorDriver ", err.Error())
	}

	_, err = driver.Start()
	if err != nil {
		log.Infoln("Got error:", err)
		return
	}
	log.Infoln("Executor process has started and running.")
	driver.Join()
}
