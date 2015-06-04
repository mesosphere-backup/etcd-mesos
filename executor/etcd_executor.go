// +build etcd-e

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
	"os"
	goexec "os/exec"
	"strings"
	"sync"
	"time"

	log "github.com/golang/glog"
	"github.com/mesos/mesos-go/executor"
	mesos "github.com/mesos/mesos-go/mesosproto"
)

var (
	etcdCmd = flag.String("exec", "", "Etcd command to launch")
)

type etcdExecutor struct {
	sync.Mutex
	cancelSuicide chan struct{}
	tasksLaunched int
}

func newEtcdExecutor() *etcdExecutor {
	return &etcdExecutor{
		cancelSuicide: make(chan struct{}),
		tasksLaunched: 0,
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
	e.Lock()
	close(e.cancelSuicide)
	e.cancelSuicide = make(chan struct{})
	e.Unlock()
}

func (e *etcdExecutor) Disconnected(executor.ExecutorDriver) {
	log.Infoln("Executor disconnected.")
	go func() {
		select {
		case <-e.cancelSuicide:
		case <-time.After(120 * time.Second):
			log.Fatalf("Lost connection to slave for 120 seconds.  Exiting.")
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

	runStatus := &mesos.TaskStatus{
		TaskId: taskInfo.GetTaskId(),
		State:  mesos.TaskState_TASK_RUNNING.Enum(),
	}
	_, err := driver.SendStatusUpdate(runStatus)
	if err != nil {
		log.Infoln("Got error", err)
	}

	e.tasksLaunched++
	log.Infoln("Total tasks launched ", e.tasksLaunched)

	go func() {
		log.Infoln("calling command: ", *etcdCmd)
		parts := strings.Fields(*etcdCmd)
		head := parts[0]
		tail := parts[1:len(parts)]
		command := goexec.Command(head, tail...)
		command.Stdout = os.Stdout
		command.Stderr = os.Stdout
		command.Start()
		command.Wait() // TODO figure out semantics

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

// -------------------------- entrypoints ----------------- //
func init() {
	flag.Parse()
}

func main() {
	log.Infoln("Starting Etcd Executor")

	dconfig := executor.DriverConfig{
		Executor: newEtcdExecutor(),
	}
	driver, err := executor.NewMesosExecutorDriver(dconfig)

	if err != nil {
		log.Infoln("Unable to create a ExecutorDriver ", err.Error())
	}

	_, err = driver.Start()
	if err != nil {
		log.Infoln("Got error:", err)
		return
	}
	log.Infoln("Executor process has started and running.")
	driver.Join()
}
