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
	"strconv"
	gotesting "testing"

	"github.com/coreos/etcd/etcdserver/etcdhttp/httptypes"
	"github.com/gogo/protobuf/proto"
	mesos "github.com/mesos/mesos-go/mesosproto"
	util "github.com/mesos/mesos-go/mesosutil"
	"github.com/stretchr/testify/assert"

	"github.com/mesosphere/etcd-mesos/config"
	emtesting "github.com/mesosphere/etcd-mesos/testing"
)

func NewOffer(id string) *mesos.Offer {
	return &mesos.Offer{
		Id:          util.NewOfferID(id),
		FrameworkId: util.NewFrameworkID("test-etcd-framework"),
		SlaveId:     util.NewSlaveID("slave-" + id),
		Hostname:    proto.String("localhost"),
		Resources: []*mesos.Resource{
			util.NewScalarResource("cpus", cpusPerTask),
			util.NewScalarResource("mem", memPerTask),
			util.NewScalarResource("disk", diskPerTask),
			util.NewRangesResource("ports", []*mesos.Value_Range{
				util.NewValueRange(uint64(0), uint64(65535)),
			}),
		},
	}
}

func TestStartup(t *gotesting.T) {
	mockdriver := &MockSchedulerDriver{}
	testScheduler := NewEtcdScheduler(1, 0, []*mesos.CommandInfo_URI{}, false)
	testScheduler.running = map[string]*config.Node{
		"etcd-1": nil,
		"etcd-2": nil,
	}

	// On registration, ReconcileTasks should be called.
	mockdriver.On(
		"ReconcileTasks",
		[]*mesos.TaskStatus{},
	).Return(mesos.Status_DRIVER_RUNNING, nil).Once()

	testScheduler.Registered(
		mockdriver,
		util.NewFrameworkID("framework-1"),
		util.NewMasterInfo("master-1", 0, 0),
	)

	assert.Equal(t, Immutable, testScheduler.state,
		"Scheduler should be placed in the Immutable state after registration "+
			"as we wait for status updates to arrive in response to ReconcileTasks.")

	assert.Equal(t, 0, len(testScheduler.running),
		"Scheduler's running list should be cleared on registration, "+
			"to be populated by ReconcileTasks.")
	mockdriver.AssertExpectations(t)
}

func TestReconciliationOnStartup(t *gotesting.T) {
	testScheduler := NewEtcdScheduler(3, 0, []*mesos.CommandInfo_URI{}, false)
	mockdriver := &MockSchedulerDriver{
		runningStatuses: make(chan *mesos.TaskStatus, 10),
		scheduler:       testScheduler,
	}

	// Valid reconciled tasks should be added to the running list.
	mockdriver.On(
		"ReconcileTasks",
		[]*mesos.TaskStatus{},
	).Return(mesos.Status_DRIVER_RUNNING, nil).Once()

	for _, taskStatus := range []*mesos.TaskStatus{
		util.NewTaskStatus(
			util.NewTaskID("etcd-1 localhost 0 0"),
			mesos.TaskState_TASK_RUNNING,
		),
		util.NewTaskStatus(
			util.NewTaskID("etcd-2 localhost 0 0"),
			mesos.TaskState_TASK_RUNNING,
		),
		util.NewTaskStatus(
			util.NewTaskID("etcd-3 localhost 0 0"),
			mesos.TaskState_TASK_RUNNING,
		),
	} {
		mockdriver.runningStatuses <- taskStatus
	}

	testScheduler.Registered(
		mockdriver,
		util.NewFrameworkID("framework-1"),
		util.NewMasterInfo("master-1", 0, 0),
	)

	assert.Equal(t, 3, len(testScheduler.running),
		"Scheduler should reconcile tasks properly.")

	mockdriver.AssertExpectations(t)
}

func TestGrowToDesiredAfterReconciliation(t *gotesting.T) {
	testScheduler := NewEtcdScheduler(3, 0, []*mesos.CommandInfo_URI{}, false)
	mockdriver := &MockSchedulerDriver{
		runningStatuses: make(chan *mesos.TaskStatus, 10),
		scheduler:       testScheduler,
	}
	testScheduler.state = Mutable
	testScheduler.healthCheck = func(map[string]*config.Node) error {
		return nil
	}

	// Push more than enough offers to shoot self in foot if unchecked.
	for _, offer := range []*mesos.Offer{
		NewOffer("1"),
		NewOffer("2"),
		NewOffer("3"),
	} {
		testScheduler.offerCache.Push(offer)
	}
	memberList := config.ClusterMemberList{
		Members: []httptypes.Member{
			{
				ID:         "1",
				Name:       "etcd-1",
				PeerURLs:   nil,
				ClientURLs: nil,
			},
			{
				ID:         "2",
				Name:       "etcd-2",
				PeerURLs:   nil,
				ClientURLs: nil,
			},
		},
	}

	_, port1, err := emtesting.NewTestEtcdServer(t, memberList)
	if err != nil {
		t.Fatalf("Failed to create test etcd server: %s", err)
	}

	_, port2, err := emtesting.NewTestEtcdServer(t, memberList)
	if err != nil {
		t.Fatalf("Failed to create test etcd server: %s", err)
	}

	// Valid reconciled tasks should be added to the running list.
	mockdriver.On(
		"ReconcileTasks",
		[]*mesos.TaskStatus{},
	).Return(mesos.Status_DRIVER_RUNNING, nil).Once()

	for _, taskStatus := range []*mesos.TaskStatus{
		util.NewTaskStatus(
			util.NewTaskID("etcd-1 localhost 0 "+strconv.Itoa(int(port1))),
			mesos.TaskState_TASK_RUNNING,
		),
		util.NewTaskStatus(
			util.NewTaskID("etcd-2 localhost 0 "+strconv.Itoa(int(port2))),
			mesos.TaskState_TASK_RUNNING,
		),
	} {
		mockdriver.runningStatuses <- taskStatus
	}

	// Scheduler should grow cluster to desired number of nodes.
	offer := NewOffer("1")
	mockdriver.On(
		"LaunchTasks",
		[]*mesos.OfferID{
			offer.Id,
		},
		[]*mesos.TaskInfo{
			{
				Resources: []*mesos.Resource{
					util.NewScalarResource("cpus", cpusPerTask),
					util.NewScalarResource("mem", memPerTask),
					util.NewScalarResource("disk", diskPerTask),
					util.NewRangesResource("ports", []*mesos.Value_Range{
						util.NewValueRange(uint64(0), uint64(1)),
					}),
				},
			},
		},
		&mesos.Filters{
			RefuseSeconds: proto.Float64(1),
		},
	).Return(mesos.Status_DRIVER_RUNNING, nil).Once()

	// Simulate failover, registration and time passing.
	mockdriver.ReconcileTasks([]*mesos.TaskStatus{})
	testScheduler.launchOne(mockdriver)
	testScheduler.launchOne(mockdriver)
	testScheduler.launchOne(mockdriver)
	testScheduler.launchOne(mockdriver)
	testScheduler.launchOne(mockdriver)

	assert.Equal(t, 3, len(testScheduler.running),
		"Scheduler should reconcile tasks properly.")

	mockdriver.AssertExpectations(t)
}

func TestScheduler(t *gotesting.T) {
	mockdriver := &MockSchedulerDriver{}

	ntasks := 1
	chillFactor := 0
	testScheduler := NewEtcdScheduler(
		ntasks,
		chillFactor,
		[]*mesos.CommandInfo_URI{},
		false,
	)

	// Skip initialization logic, tested in TestStartup.
	testScheduler.state = Mutable

	taskStatus_task_starting := util.NewTaskStatus(
		util.NewTaskID("etcd-1 localhost 1 1"),
		mesos.TaskState_TASK_RUNNING,
	)
	testScheduler.StatusUpdate(mockdriver, taskStatus_task_starting)

	taskStatus_task_running := util.NewTaskStatus(
		util.NewTaskID("etcd-1 localhost 1 1"),
		mesos.TaskState_TASK_RUNNING,
	)
	testScheduler.StatusUpdate(mockdriver, taskStatus_task_running)

	taskStatus_task_failed := util.NewTaskStatus(
		util.NewTaskID("etcd-1 localhost 1 1"),
		mesos.TaskState_TASK_FAILED,
	)
	testScheduler.StatusUpdate(mockdriver, taskStatus_task_failed)

	//assert that mock was invoked
	mockdriver.AssertExpectations(t)
}
