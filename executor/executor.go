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
		`--initial-cluster-state={{.Type}} ` +
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
}

type EtcdParams struct {
	config.Node
	Cluster string
}

// New returns an an implementation of an etcd Mesos executor that runs the
// given command when tasks are launched.
func New() executor.Executor {
	return &Executor{cancelSuicide: make(chan struct{}), shutdown: func() { os.Exit(1) }}
}

func (e *Executor) Registered(_ executor.ExecutorDriver, _ *mesos.ExecutorInfo, _ *mesos.FrameworkInfo, si *mesos.SlaveInfo) {
	log.Infoln("Registered Executor on slave ", si.GetHostname())
}

func (e *Executor) Reregistered(_ executor.ExecutorDriver, si *mesos.SlaveInfo) {
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

func (e *Executor) LaunchTask(dv executor.ExecutorDriver, ti *mesos.TaskInfo) {
	defer log.Flush()
	e.tasksLaunched++
	log.Infof("Launching task #%d %q with command %q", ti.GetName(), ti.Command.GetValue())

	var running []*config.Node
	err := json.Unmarshal(ti.Data, &running)
	if err != nil {
		log.Errorf("Could not deserialize running nodes list: %v", err)
		handleFailure(dv, ti)
		return
	}

	if len(running) == 0 {
		log.Error("Received empty running nodes list. The first element is " +
			"assumed to be our own configuration, so this is invalid.")
		handleFailure(dv, ti)
		return
	}

	cmd, err := command(running...)
	if err != nil {
		log.Errorf("Failed to create configuration for etcd: %v", err)
		handleFailure(dv, ti)
		return
	}
	log.Infof("Running command: %s", cmd)

	// TODO(tyler) the args to ConfigureInstance should probably be changed
	// to just accept a []*config.Node instead of map[string]*config.Node
	runningMap := map[string]*config.Node{}
	for i, r := range running {
		// Skip first element because we haven't started it yet.
		if i > 0 {
			runningMap[string(i)] = r
		}
	}
	err = rpc.ConfigureInstance(runningMap, running[0])
	if err != nil {
		log.Errorf("Could not configure new etcd instance, cannot continue: %v", err)
		handleFailure(dv, ti)
		return
	}

	go func() {
		defer log.Flush()
		log.Infoln("calling command: ", cmd)
		parts := strings.Fields(cmd)
		head, tail := parts[0], parts[1:]
		command := exec.Command(head, tail...)
		command.Stdout = os.Stdout
		command.Stderr = os.Stdout
		command.Start()

		runStatus := &mesos.TaskStatus{
			TaskId: ti.GetTaskId(),
			State:  mesos.TaskState_TASK_RUNNING.Enum(),
		}
		_, err := dv.SendStatusUpdate(runStatus)
		if err != nil {
			log.Infoln("Got error sending status update: ", err)
		}

		go snapshotter(running[0])
		go http.ListenAndServe(fmt.Sprintf(":%d", running[0].HTTPPort), nil)

		err = command.Wait()
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				// TODO: process exited with non-zero status
				log.Errorf("cmd was: %s", cmd)
				log.Errorf("Process exited with nonzero exit status: %+v", exitError)
			} else {
				// TODO: I/O problems...
			}
		}

		// TODO add monitoring

		// finish task
		log.Infoln("Finishing task", ti.GetName())
		finStatus := &mesos.TaskStatus{
			TaskId: ti.GetTaskId(),
			State:  mesos.TaskState_TASK_FINISHED.Enum(),
		}
		_, err = dv.SendStatusUpdate(finStatus)
		if err != nil {
			log.Infoln("Aborting after error ", err)
			dv.Abort()
			return
		}

		log.Infoln("Task finished", ti.GetName())
	}()
}

func handleFailure(dv executor.ExecutorDriver, ti *mesos.TaskInfo) {
	finStatus := &mesos.TaskStatus{
		TaskId: ti.GetTaskId(),
		State:  mesos.TaskState_TASK_FAILED.Enum(),
	}
	_, err := dv.SendStatusUpdate(finStatus)
	if err != nil {
		log.Infoln("Failed to send final status update: ", err)
	}
	dv.Abort()
}

func snapshotter(config *config.Node) {

}

func (e *Executor) KillTask(_ executor.ExecutorDriver, t *mesos.TaskID) {
	log.Infof("KillTask: not implemented: %v", t)
}

func (e *Executor) FrameworkMessage(dv executor.ExecutorDriver, msg string) {
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
		log.Errorf("formatting node: %+v", n)
		cluster = append(cluster, fmt.Sprintf("%s=http://%s:%d", n.Name, n.Host, n.RPCPort))
	}

	var out bytes.Buffer
	err := cmdTemplate.Execute(&out, EtcdParams{
		Node:    *nodes[0],
		Cluster: strings.Join(cluster, ","),
	})
	return out.String(), err
}
