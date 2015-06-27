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

package scheduler

import (
	mesos "github.com/mesos/mesos-go/mesosproto"
	util "github.com/mesos/mesos-go/mesosutil"
	"github.com/stretchr/testify/mock"
)

type MockSchedulerDriver struct {
	scheduler       *EtcdScheduler
	offers          chan *mesos.Offer
	runningStatuses chan *mesos.TaskStatus
	mock.Mock
}

func (m *MockSchedulerDriver) Init() error {
	args := m.Called()
	return args.Error(0)
}
func (m *MockSchedulerDriver) Start() (mesos.Status, error) {
	args := m.Called()
	return status(args, 0), args.Error(1)
}
func (m *MockSchedulerDriver) Stop(b bool) (mesos.Status, error) {
	args := m.Called(b)
	return status(args, 0), args.Error(1)
}
func (m *MockSchedulerDriver) Abort() (mesos.Status, error) {
	args := m.Called()
	return status(args, 0), args.Error(1)
}
func (m *MockSchedulerDriver) Join() (mesos.Status, error) {
	args := m.Called()
	return status(args, 0), args.Error(1)
}
func (m *MockSchedulerDriver) Run() (mesos.Status, error) {
	args := m.Called()
	return status(args, 0), args.Error(1)
}
func (m *MockSchedulerDriver) RequestResources(r []*mesos.Request) (mesos.Status, error) {
	args := m.Called(r)
	return status(args, 0), args.Error(1)
}
func (m *MockSchedulerDriver) ReconcileTasks(statuses []*mesos.TaskStatus) (mesos.Status, error) {
	// Send status updates for each "running" task.
	if m.scheduler != nil && m.runningStatuses != nil {
		for {
			select {
			case runningStatus := <-m.runningStatuses:
				m.scheduler.StatusUpdate(m, runningStatus)
			default:
				goto L
			}
		}
	L:
	}
	args := m.Called(statuses)
	return status(args, 0), args.Error(1)
}
func (m *MockSchedulerDriver) LaunchTasks(offerIds []*mesos.OfferID, ti []*mesos.TaskInfo, f *mesos.Filters) (mesos.Status, error) {
	if m.scheduler != nil {
		for _, taskInfo := range ti {
			status := util.NewTaskStatus(
				taskInfo.TaskId,
				mesos.TaskState_TASK_RUNNING,
			)
			// TODO(tyler) use actual executor here to launch a test instance, so we can catch etcd config errors
			m.scheduler.StatusUpdate(m, status)
		}
	}

	// Too much dynamic stuff for comparison, just look at Resources.
	tasks := []*mesos.TaskInfo{
		{
			Resources: ti[0].Resources,
		},
	}
	args := m.Called(offerIds, tasks, f)
	return status(args, 0), args.Error(1)
}
func (m *MockSchedulerDriver) KillTask(tid *mesos.TaskID) (mesos.Status, error) {
	args := m.Called(tid)
	return status(args, 0), args.Error(1)
}
func (m *MockSchedulerDriver) DeclineOffer(oid *mesos.OfferID, f *mesos.Filters) (mesos.Status, error) {
	args := m.Called(oid, f)
	return status(args, 0), args.Error(1)
}
func (m *MockSchedulerDriver) ReviveOffers() (mesos.Status, error) {
	args := m.Called()
	return status(args, 0), args.Error(0)
}
func (m *MockSchedulerDriver) SendFrameworkMessage(eid *mesos.ExecutorID, sid *mesos.SlaveID, s string) (mesos.Status, error) {
	args := m.Called(eid, sid, s)
	return status(args, 0), args.Error(1)
}
func (m *MockSchedulerDriver) Destroy() {
	m.Called()
}
func (m *MockSchedulerDriver) Wait() {
	m.Called()
}

func status(args mock.Arguments, at int) (val mesos.Status) {
	if x := args.Get(at); x != nil {
		val = x.(mesos.Status)
	}
	return
}
