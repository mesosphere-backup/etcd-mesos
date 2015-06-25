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

type etcdExecutor struct {
	cancelSuicide chan struct{}
	tasksLaunched int
	cmd           string
	shutdown      func()
}

func New(cmd string) *etcdExecutor {
	return &etcdExecutor{
		cancelSuicide: make(chan struct{}),
		cmd:           cmd,
		shutdown:      func() { os.Exit(1) },
	}
}

func (e *etcdExecutor) Registered(
	driver executor.ExecutorDriver,
	execInfo *mesos.ExecutorInfo,
	fwinfo *mesos.FrameworkInfo,
	slaveInfo *mesos.SlaveInfo,
) {
	log.Infoln("Registered Executor on slave ", slaveInfo.GetHostname())
}

func (e *etcdExecutor) Reregistered(
	driver executor.ExecutorDriver,
	slaveInfo *mesos.SlaveInfo,
) {
	log.Infoln("Re-registered Executor on slave ", slaveInfo.GetHostname())

	// Coax any jumpers back from the cliff.
	cancelOne := func() bool {
		select {
		case e.cancelSuicide <- struct{}{}:
			return true
		default:
			return false
		}
	}
	// Cancel until none left, although more than once should not happen.
	for cancelOne() {
	}
}

func (e *etcdExecutor) Disconnected(executor.ExecutorDriver) {
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

func (e *etcdExecutor) LaunchTask(
	driver executor.ExecutorDriver,
	taskInfo *mesos.TaskInfo,
) {
	log.Infoln(
		"Launching task",
		taskInfo.GetName(),
		"with command",
		taskInfo.Command.GetValue(),
	)

	e.tasksLaunched++
	log.Infoln("Total tasks launched ", e.tasksLaunched)

	go func() {
		log.Infoln("calling command: ", e.cmd)
		parts := strings.Fields(e.cmd)
		head, tail := parts[0], parts[1:]
		command := exec.Command(head, tail...)
		command.Stdout = os.Stdout
		command.Stderr = os.Stdout
		command.Start()

		runStatus := &mesos.TaskStatus{
			TaskId: taskInfo.GetTaskId(),
			State:  mesos.TaskState_TASK_RUNNING.Enum(),
		}
		_, err := driver.SendStatusUpdate(runStatus)
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
		log.Infoln("Finishing task", taskInfo.GetName())
		finStatus := &mesos.TaskStatus{
			TaskId: taskInfo.GetTaskId(),
			State:  mesos.TaskState_TASK_FINISHED.Enum(),
		}
		_, err = driver.SendStatusUpdate(finStatus)
		if err != nil {
			log.Infoln("Aborting after error ", err)
			driver.Abort()
		}
		log.Infoln("Task finished", taskInfo.GetName())
	}()
}

func (e *etcdExecutor) KillTask(executor.ExecutorDriver, *mesos.TaskID) {
	log.Infoln("Kill task")
}

func (e *etcdExecutor) FrameworkMessage(
	driver executor.ExecutorDriver,
	msg string,
) {
	log.Infoln("Got framework message: ", msg)
}

func (e *etcdExecutor) Shutdown(executor.ExecutorDriver) {
	log.Infoln("Shutting down the executor")
}

func (e *etcdExecutor) Error(driver executor.ExecutorDriver, err string) {
	log.Infoln("Got error message:", err)
}
