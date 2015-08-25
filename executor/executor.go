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

package executor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"text/template"
	"time"

	log "github.com/golang/glog"
	"github.com/mesos/mesos-go/executor"
	mesos "github.com/mesos/mesos-go/mesosproto"

	"github.com/mesosphere/etcd-mesos/config"
	"github.com/mesosphere/etcd-mesos/rpc"
)

const (
	defaultMesosMaxSlavePingTimeouts = 5
	defaultMesosSlavePingTimeout     = 15 * time.Second
)

var cmdTemplate = template.Must(template.New("etcd-cmd").Parse(
	`./etcd --data-dir=etcd_data --name={{.Name}} ` +
		`--listen-peer-urls=http://{{.Host}}:{{.RPCPort}} ` +
		`--initial-advertise-peer-urls=http://{{.Host}}:{{.RPCPort}} ` +
		`--listen-client-urls=http://{{.Host}}:{{.ClientPort}} ` +
		`--advertise-client-urls=http://{{.Host}}:{{.ClientPort}} ` +
		`--initial-cluster={{.Cluster}}`,
))

type Executor struct {
	cancelSuicide chan struct{}
	tasksLaunched int
	shutdown      func()
	launchTimeout time.Duration
	shutdownChan  chan struct{}
}

type EtcdParams struct {
	config.Node
	Cluster string
}

// New returns an an implementation of an etcd Mesos executor that runs the
// given command when tasks are launched.
func New(launchTimeout time.Duration) executor.Executor {
	e := &Executor{
		cancelSuicide: make(chan struct{}),
		launchTimeout: launchTimeout,
		shutdownChan:  make(chan struct{}),
	}
	e.shutdown = func() {
		close(e.shutdownChan)
		os.Exit(1)
	}
	return e
}

func (e *Executor) Registered(
	_ executor.ExecutorDriver,
	_ *mesos.ExecutorInfo,
	_ *mesos.FrameworkInfo,
	si *mesos.SlaveInfo,
) {
	log.Infoln("Registered Executor on slave ", si.GetHostname())
}

func (e *Executor) Reregistered(
	_ executor.ExecutorDriver,
	si *mesos.SlaveInfo,
) {
	log.Infoln("Re-registered Executor on slave ", si.GetHostname())

	// Cancel until none left, although more than once should not happen.
	for {
		select {
		case e.cancelSuicide <- struct{}{}:
		default:
			return
		}
	}
}

func (e *Executor) Disconnected(_ executor.ExecutorDriver) {
	log.Infoln("Executor disconnected.")

	const suicideTimeout = ((defaultMesosMaxSlavePingTimeouts + 1) *
		defaultMesosSlavePingTimeout)

	go func() {
		select {
		case <-e.cancelSuicide:
		case <-time.After(suicideTimeout):
			log.Errorf("Lost connection to local slave for %s seconds. "+
				"This is longer than the mesos master<->slave timeout, so "+
				"we cannot avoid termination at this point. Exiting.",
				suicideTimeout/time.Second)
			if e.shutdown != nil {
				e.shutdown()
			}
		}
	}()
}

func (e *Executor) LaunchTask(
	driver executor.ExecutorDriver,
	taskInfo *mesos.TaskInfo,
) {
	defer log.Flush()
	e.tasksLaunched++
	var running []*config.Node
	err := json.Unmarshal(taskInfo.Data, &running)
	if err != nil {
		log.Errorf("Could not deserialize running nodes list: %v", err)
		handleFailure(driver, taskInfo)
		return
	}

	if len(running) == 0 {
		log.Error("Received empty running nodes list. The first element is " +
			"assumed to be our own configuration, so this is invalid.")
		handleFailure(driver, taskInfo)
		return
	}

	go e.etcdHarness(taskInfo, running, driver, running[0])

}

func dumbExec(args string) {
	log.Infof("running command %s", args)
	argv := strings.Fields(args)
	c := exec.Command(argv[0], argv[1:]...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Start()
	c.Wait()
}

func (e *Executor) etcdHarness(
	taskInfo *mesos.TaskInfo,
	running []*config.Node,
	driver executor.ExecutorDriver,
	node *config.Node,
) {
	defer log.Flush()
	cmd, err := command(running...)
	if err != nil {
		log.Errorf("Failed to create configuration for etcd: %v", err)
		handleFailure(driver, taskInfo)
		return
	}
	cmd += " --initial-cluster-state=" + node.Type

	runningMap := map[string]*config.Node{}
	for i, r := range running {
		// Skip first element because we haven't started it yet.
		if i > 0 {
			runningMap[string(i)] = r
		}
	}
	err = rpc.ConfigureInstance(runningMap, running[0])
	if err != nil {
		log.Errorf("Could not configure etcd instance, cannot continue: %v", err)
		handleFailure(driver, taskInfo)
		return
	}

	reseedChan := make(chan struct{}, 1)
	go e.reseedListener(node, reseedChan)

	runStatus := &mesos.TaskStatus{
		TaskId: taskInfo.GetTaskId(),
		State:  mesos.TaskState_TASK_RUNNING.Enum(),
	}
	_, err = driver.SendStatusUpdate(runStatus)
	if err != nil {
		log.Errorf("Got error sending status update, terminating: ", err)
		handleFailure(driver, taskInfo)
	}

	// Run etcd, but if we receive a reseed request over http
	// then we must terminate the process, configure it to be
	// the only node, and tell it to re-seed as a new instance.
	// If a process exits early, retry until launchTimeout.
	now := time.Now()
	before := &now
	delay := 0
	for {
		killChan := make(chan struct{})
		exitChan := make(chan struct{})

		go runUntilClosed(driver, taskInfo, cmd, killChan, exitChan)

		select {
		case <-reseedChan:
			// We've received an http request to reseed
			close(killChan)

			// Strip out existing membership info
			dumbExec("./etcdctl backup " +
				"--data-dir=./etcd_data " +
				"--backup-dir=./etcd_backup")

			// Move backup dir over old data dir
			dumbExec("rm -rf ./etcd_data")
			dumbExec("mv ./etcd_data ./etcd_backup")

			// Restart etcd with --force-new-cluster=true
			cmd, err = command(node)
			if err != nil {
				log.Errorf("Failed to create configuration for etcd: %v", err)
				handleFailure(driver, taskInfo)
			}
			cmd += " --force-new-cluster=true"
			// Restart the launch timeout window.
			*before = time.Now()
		case <-e.shutdownChan:
			// The executor is shutting down, so we should kill etcd.
			close(killChan)
		case <-exitChan:
			// We've exited early.  This may be because of a port being
			// allocated to a previous instance after a reseed attempt.
			if time.Since(*before) > e.launchTimeout {
				log.Errorf("We've exceeded the launch timeout of %v, exiting.",
					e.launchTimeout)
				handleFailure(driver, taskInfo)
				if e.shutdown != nil {
					e.shutdown()
				}
			}
			// linear truncated backoff
			time.Sleep(time.Duration(delay) * time.Second)
			delay += 1
			delay = int(math.Min(float64(delay), 4))
		}
	}
}

func runUntilClosed(
	driver executor.ExecutorDriver,
	taskInfo *mesos.TaskInfo,
	cmd string,
	killChan chan struct{},
	exitChan chan struct{},
) {
	log.Infoln("calling command: ", cmd)
	parts := strings.Fields(cmd)
	head, tail := parts[0], parts[1:]
	command := exec.Command(head, tail...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stdout
	command.Start()

	go func() {
		command.Wait()
		log.Warning("etcd process exited")
		select {
		case <-killChan:
		default:
			close(exitChan)
		}
	}()

	<-killChan
	command.Process.Kill()
}

func handleFailure(
	driver executor.ExecutorDriver,
	taskInfo *mesos.TaskInfo,
) {
	finStatus := &mesos.TaskStatus{
		TaskId: taskInfo.GetTaskId(),
		State:  mesos.TaskState_TASK_FAILED.Enum(),
	}
	_, err := driver.SendStatusUpdate(finStatus)
	if err != nil {
		log.Infoln("Failed to send final status update: ", err)
	}
	driver.Abort()
}

func (e *Executor) reseedListener(
	node *config.Node,
	reseedChan chan struct{},
) {
	mux := http.NewServeMux()
	mux.HandleFunc("/reseed", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "ok")
		log.Warning("Received reseed request!")
		reseedChan <- struct{}{}
	})
	log.Infof("Listening for requests to reseed on port %d", node.ReseedPort)
	log.Error(http.ListenAndServe(fmt.Sprintf(":%d", node.ReseedPort), mux))
	if e.shutdown != nil {
		e.shutdown()
	}
}

func (e *Executor) KillTask(driver executor.ExecutorDriver, t *mesos.TaskID) {
	log.Infof("KillTask received!  Shutting down!")
	if e.shutdown != nil {
		e.shutdown()
	}
}

func (e *Executor) FrameworkMessage(
	driver executor.ExecutorDriver,
	msg string,
) {
	log.Infof("FrameworkMessage: %s", msg)
}

func (e *Executor) Shutdown(_ executor.ExecutorDriver) {
	log.Infoln("Shutting down the executor")
}

func (e *Executor) Error(_ executor.ExecutorDriver, err string) {
	log.Infof("Error: %s", err)
}

// command returns an etcd command templated with the given Nodes.
// ASSUMPTION: the first node in the nodes list is the local one.
func command(nodes ...*config.Node) (string, error) {
	if len(nodes) == 0 {
		return "", errors.New("No nodes to configure.")
	}

	cluster := make([]string, 0, len(nodes))
	for _, n := range nodes {
		log.Infof("formatting node: %+v", n)
		cluster = append(cluster,
			fmt.Sprintf("%s=http://%s:%d", n.Name, n.Host, n.RPCPort))
	}

	var out bytes.Buffer
	err := cmdTemplate.Execute(&out, EtcdParams{
		Node:    *nodes[0],
		Cluster: strings.Join(cluster, ","),
	})
	return out.String(), err
}
