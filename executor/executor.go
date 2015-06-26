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
	"os"
	"os/exec"
	"strings"
	"time"

	log "github.com/golang/glog"
	"github.com/mesos/mesos-go/executor"
	mesos "github.com/mesos/mesos-go/mesosproto"
)

const (
	defaultMesosMaxSlavePingTimeouts = 5
	defaultMesosSlavePingTimeout     = 15 * time.Second
)

type Executor struct {
	cancelSuicide chan struct{}
	tasksLaunched int
	cmd           string
	shutdown      func()
}

// New returns an an implementation of an etcd Mesos executor that runs the
// given command when tasks are launched.
func New(cmd string) executor.Executor {
	return &Executor{cancelSuicide: make(chan struct{}), cmd: cmd, shutdown: func() { os.Exit(1) }}
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
	e.tasksLaunched++
	log.Infof("Launching task #%d %q with command %q", ti.GetName(), ti.Command.GetValue())

	go func() {
		log.Infoln("calling command: ", e.cmd)
		parts := strings.Fields(e.cmd)
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

		err = command.Wait()
		if err != nil {
			if exitError, ok := err.(*exec.ExitError); ok {
				// TODO: process exited with non-zero status
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
