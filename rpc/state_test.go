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

package rpc

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMasterState(t *testing.T) {
	masterState := &MasterState{}
	err := json.Unmarshal([]byte(state), masterState)
	assert.Nil(t, err)
	assert.Equal(t, 3, len(masterState.Frameworks[0].Tasks))
	fmt.Printf("task 1: %+v\n", masterState.Frameworks[0].Tasks[0])
}

const state = `
{
    "activated_slaves": 3,
    "build_date": "2015-05-14 16:24:13",
    "build_time": 1431645853,
    "build_user": "tyler",
    "completed_frameworks": [],
    "deactivated_slaves": 0,
    "elected_time": 1436896187.91769,
    "failed_tasks": 0,
    "finished_tasks": 0,
    "flags": {
        "allocation_interval": "1secs",
        "authenticate": "false",
        "authenticate_slaves": "false",
        "authenticators": "crammd5",
        "framework_sorter": "drf",
        "help": "false",
        "hostname": "localhost",
        "initialize_driver_logging": "true",
        "ip": "127.0.0.1",
        "log_auto_initialize": "true",
        "logbufsecs": "0",
        "logging_level": "INFO",
        "port": "5050",
        "quiet": "false",
        "quorum": "1",
        "recovery_slave_removal_limit": "100%",
        "registry": "replicated_log",
        "registry_fetch_timeout": "1mins",
        "registry_store_timeout": "5secs",
        "registry_strict": "false",
        "root_submissions": "true",
        "slave_reregister_timeout": "10mins",
        "user_sorter": "drf",
        "version": "false",
        "webui_dir": "/usr/local/share/mesos/webui",
        "work_dir": "/tmp/mesos-master",
        "zk": "zk://localhost:2181/mesos",
        "zk_session_timeout": "10secs"
    },
    "frameworks": [
        {
            "active": false,
            "checkpoint": true,
            "completed_tasks": [],
            "failover_timeout": 604800,
            "hostname": "tanbox",
            "id": "20150626-111058-16777343-5050-12504-0000",
            "name": "etcd-t1",
            "offered_resources": {
                "cpus": 0,
                "disk": 0,
                "mem": 0
            },
            "offers": [],
            "registered_time": 1436899507.7515,
            "reregistered_time": 1436899507.75158,
            "resources": {
                "cpus": 3.3,
                "disk": 3072,
                "mem": 864,
                "ports": "[32000-32002, 31000-31002, 33000-33002]"
            },
            "role": "*",
            "tasks": [
                {
                    "executor_id": "etcd-1436847322",
                    "framework_id": "20150626-111058-16777343-5050-12504-0000",
                    "id": "etcd-1436847322 localhost.localdomain 33000 33001 33002",
                    "labels": [],
                    "name": "etcd-1436847322",
                    "resources": {
                        "cpus": 1,
                        "disk": 1024,
                        "mem": 256,
                        "ports": "[33000-33002]"
                    },
                    "slave_id": "20150713-202443-16777343-5050-6850-S20",
                    "state": "TASK_RUNNING",
                    "statuses": [
                        {
                            "state": "TASK_RUNNING",
                            "timestamp": 1436847329
                        }
                    ]
                },
                {
                    "executor_id": "etcd-1436847948",
                    "framework_id": "20150626-111058-16777343-5050-12504-0000",
                    "id": "etcd-1436847948 localhost.localdomain 31000 31001 31002",
                    "labels": [],
                    "name": "etcd-1436847948",
                    "resources": {
                        "cpus": 1,
                        "disk": 1024,
                        "mem": 256,
                        "ports": "[31000-31002]"
                    },
                    "slave_id": "20150713-202443-16777343-5050-6850-S23",
                    "state": "TASK_RUNNING",
                    "statuses": [
                        {
                            "state": "TASK_RUNNING",
                            "timestamp": 1436847957
                        }
                    ]
                },
                {
                    "executor_id": "etcd-1436847323",
                    "framework_id": "20150626-111058-16777343-5050-12504-0000",
                    "id": "etcd-1436847323 localhost.localdomain 32000 32001 32002",
                    "labels": [],
                    "name": "etcd-1436847323",
                    "resources": {
                        "cpus": 1,
                        "disk": 1024,
                        "mem": 256,
                        "ports": "[32000-32002]"
                    },
                    "slave_id": "20150713-202443-16777343-5050-6850-S21",
                    "state": "TASK_RUNNING",
                    "statuses": [
                        {
                            "state": "TASK_RUNNING",
                            "timestamp": 1436847336
                        }
                    ]
                }
            ],
            "unregistered_time": 0,
            "used_resources": {
                "cpus": 3.3,
                "disk": 3072,
                "mem": 864,
                "ports": "[32000-32002, 31000-31002, 33000-33002]"
            },
            "user": "tyler",
            "webui_url": ""
        }
    ],
    "git_branch": "refs/heads/0.22.1",
    "git_sha": "d6309f92a7f9af3ab61a878403e3d9c284ea87e0",
    "git_tag": "0.22.1",
    "hostname": "localhost",
    "id": "20150714-104947-16777343-5050-5520",
    "killed_tasks": 0,
    "leader": "master@127.0.0.1:5050",
    "lost_tasks": 0,
    "orphan_tasks": [],
    "pid": "master@127.0.0.1:5050",
    "slaves": [
        {
            "active": true,
            "attributes": {},
            "hostname": "localhost.localdomain",
            "id": "20150713-202443-16777343-5050-6850-S21",
            "pid": "slave(1)@127.0.0.1:5052",
            "registered_time": 1436896188.84498,
            "reregistered_time": 1436896188.84545,
            "resources": {
                "cpus": 4,
                "disk": 4096,
                "mem": 4096,
                "ports": "[32000-33000]"
            }
        },
        {
            "active": true,
            "attributes": {},
            "hostname": "localhost.localdomain",
            "id": "20150713-202443-16777343-5050-6850-S23",
            "pid": "slave(1)@127.0.0.1:5053",
            "registered_time": 1436896188.27448,
            "reregistered_time": 1436896188.27491,
            "resources": {
                "cpus": 4,
                "disk": 4096,
                "mem": 4096,
                "ports": "[31000-32000]"
            }
        },
        {
            "active": true,
            "attributes": {},
            "hostname": "localhost.localdomain",
            "id": "20150713-202443-16777343-5050-6850-S20",
            "pid": "slave(1)@127.0.0.1:5051",
            "registered_time": 1436896188.04256,
            "reregistered_time": 1436896188.04355,
            "resources": {
                "cpus": 4,
                "disk": 4096,
                "mem": 4096,
                "ports": "[33000-34000]"
            }
        }
    ],
    "staged_tasks": 0,
    "start_time": 1436896187.84332,
    "started_tasks": 0,
    "unregistered_frameworks": [],
    "version": "0.22.1"
}
`
