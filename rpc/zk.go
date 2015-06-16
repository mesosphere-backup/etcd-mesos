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
	"errors"
	"strings"
	"time"

	log "github.com/golang/glog"
	mesos "github.com/mesos/mesos-go/mesosproto"
	"github.com/samuel/go-zookeeper/zk"
)

func ParseZKURI(zkURI string) (servers []string, chroot string, err error) {
	servers = []string{}

	// this is to use the canonical zk://host1:ip,host2/zkChroot format
	strippedZKConnect := strings.TrimLeft(zkURI, "zk://")
	parts := strings.Split(strippedZKConnect, "/")
	if len(parts) == 2 {
		if parts[1] == "" {
			return nil, "", errors.New("ZK chroot must not be the root path \"/\"!")
		}
		chroot = "/" + parts[1]
		servers = strings.Split(parts[0], ",")
	} else if len(parts) == 1 {
		servers = strings.Split(parts[0], ",")
	} else {
		return nil, "", errors.New("ZK URI must be of the form " +
			"zk://$host1:$port1,$host2:$port2/path/to/zk/chroot")
	}
	for _, zk := range servers {
		if len(strings.Split(zk, ":")) != 2 {
			return nil, "", errors.New("ZK URI must be of the form " +
				"zk://$host1:$port1,$host2:$port2/path/to/zk/chroot")
		}
	}
	return servers, chroot, nil
}

func PersistFrameworkID(fwid *mesos.FrameworkID, zkServers []string, zkChroot string, clusterName string) error {
	c, _, err := zk.Connect(zkServers, time.Second*5)
	if err != nil {
		return err
	}
	defer c.Close()
	// attempt to create the path
	_, err = c.Create(
		zkChroot,
		[]byte(""),
		0,
		zk.WorldACL(zk.PermAll),
	)
	if err != nil && err != zk.ErrNodeExists {
		return err
	}
	// attempt to write framework ID to <path> / <clusterName>
	_, err = c.Create(zkChroot+"/"+clusterName,
		[]byte(fwid.GetValue()),
		0,
		zk.WorldACL(zk.PermAll))
	// TODO(tyler) when err is zk.ErrNodeExists, cross-check value
	if err != nil {
		return err
	}
	log.Info("Successfully persisted Framework ID to zookeeper.")

	return nil
}

func GetPreviousFrameworkID(zkServers []string, zkChroot string, clusterName string) (string, error) {
	c, _, err := zk.Connect(zkServers, time.Second*5)
	if err != nil {
		return "", err
	}
	defer c.Close()
	rawData, _, err := c.Get(zkChroot + "/" + clusterName)
	return string(rawData), err
}

// TODO(tyler) make this more testable.
func ClearZKState(zkServers []string, zkChroot string, clusterName string) error {
	c, _, err := zk.Connect(zkServers, time.Second*5)
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Delete(zkChroot+"/"+clusterName, -1)
}
