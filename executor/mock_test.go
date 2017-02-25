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
	mesos "github.com/mesos/mesos-go/api/v0/mesosproto"
	//"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockExecutorDriver struct {
	mock.Mock
}

func (m *MockExecutorDriver) Start() (mesos.Status, error) {
	args := m.Called()
	return args.Get(0).(mesos.Status), args.Error(1)
}

func (m *MockExecutorDriver) Stop() (mesos.Status, error) {
	args := m.Called()
	return args.Get(0).(mesos.Status), args.Error(1)
}

func (m *MockExecutorDriver) Abort() (mesos.Status, error) {
	args := m.Called()
	return args.Get(0).(mesos.Status), args.Error(1)
}

func (m *MockExecutorDriver) Join() (mesos.Status, error) {
	args := m.Called()
	return args.Get(0).(mesos.Status), args.Error(1)
}

func (m *MockExecutorDriver) Run() (mesos.Status, error) {
	args := m.Called()
	return args.Get(0).(mesos.Status), args.Error(1)
}

func (m *MockExecutorDriver) SendStatusUpdate(taskStatus *mesos.TaskStatus) (mesos.Status, error) {
	args := m.Called(*taskStatus.State)
	return args.Get(0).(mesos.Status), args.Error(1)
}

func (m *MockExecutorDriver) SendFrameworkMessage(msg string) (mesos.Status, error) {
	args := m.Called(msg)
	return args.Get(0).(mesos.Status), args.Error(1)
}

func status(args mock.Arguments, at int) (val mesos.Status) {
	if x := args.Get(at); x != nil {
		val = x.(mesos.Status)
	}
	return
}
