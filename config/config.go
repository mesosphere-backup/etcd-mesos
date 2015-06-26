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

package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Etcd struct {
	Name       string `json:"name"`
	Task       string `json:"task"`
	Host       string `json:"host"`
	RpcPort    uint64 `json:"rpcPort"`
	ClientPort uint64 `json:"clientPort"`
	Type       string `json:"type"`
	SlaveID    string `json:"slaveID"`
}

func Parse(input string) (*Etcd, error) {
	splits := strings.Split(input, " ")
	if len(splits) != 4 {
		return nil, errors.New("Invalid format for serialized Etcd.")
	}
	rpcPort, err := strconv.ParseUint(splits[2], 10, 64)
	if err != nil {
		return nil, err
	}
	clientPort, err := strconv.ParseUint(splits[3], 10, 64)
	if err != nil {
		return nil, err
	}

	return &Etcd{
		Name:       splits[0],
		Host:       splits[1],
		RpcPort:    rpcPort,
		ClientPort: clientPort,
	}, nil
}

func String(input *Etcd) string {
	return fmt.Sprintf(
		"%s %s %d %d",
		input.Name,
		input.Host,
		input.RpcPort,
		input.ClientPort,
	)
}