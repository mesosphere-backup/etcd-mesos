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
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gogo/protobuf/proto"
	mesos "github.com/mesos/mesos-go/mesosproto"
	util "github.com/mesos/mesos-go/mesosutil"
	"github.com/stretchr/testify/assert"

	"github.com/mesosphere/etcd-mesos/common"
	"github.com/mesosphere/etcd-mesos/rpc"
)

type TestEtcdServer struct {
	server *httptest.Server
}

func NewTestEtcdServer(t *testing.T, memberList rpc.ClusterMemberList) (*TestEtcdServer, int64, error) {
	ts := TestEtcdServer{}
	mux := http.NewServeMux()

	mux.HandleFunc("/v2/members", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		serializedMemberList, err := json.Marshal(&memberList)
		if err == nil {
			w.Write(serializedMemberList)
		} else {
			t.Fatal("Could not serialize test ClusterMemberList.")
		}
	})

	ts.server = httptest.NewServer(mux)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, 0, err
	}

	go func() {
		err = http.Serve(listener, mux)
		if err != nil {
			t.Fatalf("Test Etcd server failed! %s", err)
		}
	}()

	parts := strings.Split(listener.Addr().String(), ":")
	if len(parts) != 2 {
		return nil, 0, errors.New("Bad address: " + listener.Addr().String())
	}
	port, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, 0, errors.New("Could not parse port into an int: " + parts[1])
	}

	return &ts, port, nil
}

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

func TestStartup(t *testing.T) {
	mockdriver := &MockSchedulerDriver{}
	testScheduler := NewEtcdScheduler(1, 0, []*mesos.CommandInfo_URI{})
	testScheduler.running = map[string]*common.EtcdConfig{
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

func TestReconciliationOnStartup(t *testing.T) {
	testScheduler := NewEtcdScheduler(3, 0, []*mesos.CommandInfo_URI{})
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
			util.NewTaskID("etcd-1:localhost:0:0"),
			mesos.TaskState_TASK_RUNNING,
		),
		util.NewTaskStatus(
			util.NewTaskID("etcd-2:localhost:0:0"),
			mesos.TaskState_TASK_RUNNING,
		),
		util.NewTaskStatus(
			util.NewTaskID("etcd-3:localhost:0:0"),
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

func TestGrowToDesiredAfterReconciliation(t *testing.T) {
	testScheduler := NewEtcdScheduler(3, 0, []*mesos.CommandInfo_URI{})
	mockdriver := &MockSchedulerDriver{
		runningStatuses: make(chan *mesos.TaskStatus, 10),
		scheduler:       testScheduler,
	}
	testScheduler.state = Mutable
	testScheduler.healthCheck = func(map[string]*common.EtcdConfig) error {
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
	memberList := rpc.ClusterMemberList{
		Members: []struct {
			Id         string   `json:"id"`
			Name       string   `json:"name"`
			PeerURLS   []string `json:"peerURLS"`
			ClientURLS []string `json:"clientURLS"`
		}{
			{
				Id:         "1",
				Name:       "etcd-1",
				PeerURLS:   nil,
				ClientURLS: nil,
			},
			{
				Id:         "2",
				Name:       "etcd-2",
				PeerURLS:   nil,
				ClientURLS: nil,
			},
		},
	}

	_, port1, err := NewTestEtcdServer(t, memberList)
	if err != nil {
		t.Fatalf("Failed to create test etcd server: %s", err)
	}

	_, port2, err := NewTestEtcdServer(t, memberList)
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
			util.NewTaskID("etcd-1:localhost:0:"+strconv.Itoa(int(port1))),
			mesos.TaskState_TASK_RUNNING,
		),
		util.NewTaskStatus(
			util.NewTaskID("etcd-2:localhost:0:"+strconv.Itoa(int(port2))),
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
			&mesos.TaskInfo{
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

func ZestScheduler(t *testing.T) {
	mockdriver := &MockSchedulerDriver{}

	mockdriver.On("KillTask", util.NewTaskID("test-task-001")).Return(mesos.Status_DRIVER_RUNNING, nil)

	ntasks := 1
	chillFactor := 0
	testScheduler := NewEtcdScheduler(ntasks, chillFactor, []*mesos.CommandInfo_URI{})

	// Skip initialization logic, tested in TestStartup.
	testScheduler.state = Mutable

	taskStatus_task_starting := util.NewTaskStatus(
		util.NewTaskID("test-task-001"),
		mesos.TaskState_TASK_RUNNING,
	)
	testScheduler.StatusUpdate(mockdriver, taskStatus_task_starting)

	taskStatus_task_running := util.NewTaskStatus(
		util.NewTaskID("test-task-001"),
		mesos.TaskState_TASK_RUNNING,
	)
	testScheduler.StatusUpdate(mockdriver, taskStatus_task_running)

	taskStatus_task_failed := util.NewTaskStatus(
		util.NewTaskID("test-task-001"),
		mesos.TaskState_TASK_FAILED,
	)
	testScheduler.StatusUpdate(mockdriver, taskStatus_task_failed)

	//assert that mock was invoked
	mockdriver.AssertExpectations(t)
}
