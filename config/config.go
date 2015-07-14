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

// Node represents an etcd node's configuration.
type Node struct {
	Name       string `json:"name"`
	Task       string `json:"task"`
	Host       string `json:"host"`
	RPCPort    uint64 `json:"rpcPort"`
	ClientPort uint64 `json:"clientPort"`
	HTTPPort   uint64 `json:"httpPort"`
	Type       string `json:"type"`
	SlaveID    string `json:"slaveID"`
}

// ErrUnmarshal is returned whenever config unmarshalling
var ErrUnmarshal = errors.New("config: unmarshaling failed")

// Parse attempts to deserialize a config.Node from a byte array.
func Parse(text string) (*Node, error) {
	fs := strings.Fields(string(text))
	if len(fs) != 5 {
		return nil, ErrUnmarshal
	}
	n := &Node{Name: fs[0], Host: fs[1]}

	var err error
	if n.RPCPort, err = strconv.ParseUint(fs[2], 10, 64); err != nil {
		return nil, ErrUnmarshal
	} else if n.ClientPort, err = strconv.ParseUint(fs[3], 10, 64); err != nil {
		return nil, ErrUnmarshal
	} else if n.HTTPPort, err = strconv.ParseUint(fs[4], 10, 64); err != nil {
		return nil, ErrUnmarshal
	}

	return n, nil
}

// String implements the fmt.Stringer interface, returning a space separated
// string representation of a Node.
func (n Node) String() string {
	return fmt.Sprintf(
		"%s %s %d %d %d", n.Name, n.Host, n.RPCPort, n.ClientPort, n.HTTPPort)
}
