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
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	log "github.com/golang/glog"
	mesos "github.com/mesos/mesos-go/mesosproto"
	"github.com/samuel/go-zookeeper/zk"
)

func ParseZKURI(zkURI string) (servers []string, chroot string, err error) {
	servers = []string{}

	// this is to use the canonical zk://host1:ip,host2/zkChroot format
	strippedZKConnect := strings.TrimPrefix(zkURI, "zk://")
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

func PersistFrameworkID(
	fwid *mesos.FrameworkID,
	zkServers []string,
	zkChroot string,
	frameworkName string,
) error {
	c, _, err := zk.Connect(zkServers, RPC_TIMEOUT)
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
	// attempt to write framework ID to <path> / <frameworkName>
	_, err = c.Create(zkChroot+"/"+frameworkName+"_framework_id",
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

func UpdateReconciliationInfo(
	reconciliationInfo map[string]string,
	zkServers []string,
	zkChroot string,
	frameworkName string,
) error {
	serializedReconciliationInfo, err := json.Marshal(reconciliationInfo)
	if err != nil {
		return err
	}

	request := func() error {
		c, _, err := zk.Connect(zkServers, RPC_TIMEOUT)
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
		_, err = c.Create(zkChroot+"/"+frameworkName+"_reconciliation",
			serializedReconciliationInfo,
			0,
			zk.WorldACL(zk.PermAll),
		)
		if err != nil && err != zk.ErrNodeExists {
			return err
		}
		// attempt to write framework ID to <path> / <frameworkName>
		_, err = c.Set(zkChroot+"/"+frameworkName+"_reconciliation",
			serializedReconciliationInfo,
			-1)
		if err != nil {
			return err
		}
		log.Info("Successfully persisted reconciliation info to zookeeper.")

		return nil
	}

	var outerErr error = nil
	backoff := 1
	log.Info("persisting reconciliation info to zookeeper")
	// Use extra retries here because we really don't want to fall out of
	// sync here.
	for retries := 0; retries < RPC_RETRIES*2; retries++ {
		outerErr = request()
		if outerErr == nil {
			break
		}
		log.Warningf("Failed to configure cluster for new instance: %+v.  "+
			"Backing off for %d seconds and retrying.", outerErr, backoff)
		time.Sleep(time.Duration(backoff) * time.Second)
		backoff = int(math.Min(float64(backoff<<1), 8))
	}
	return outerErr
}

func GetPreviousFrameworkID(
	zkServers []string,
	zkChroot string,
	frameworkName string,
) (string, error) {
	c, _, err := zk.Connect(zkServers, RPC_TIMEOUT)
	if err != nil {
		return "", err
	}
	defer c.Close()
	rawData, _, err := c.Get(zkChroot + "/" + frameworkName + "_framework_id")
	return string(rawData), err
}

func GetPreviousReconciliationInfo(
	zkServers []string,
	zkChroot string,
	frameworkName string,
) (map[string]string, error) {
	c, _, err := zk.Connect(zkServers, RPC_TIMEOUT)
	if err != nil {
		return map[string]string{}, err
	}
	defer c.Close()
	rawData, _, err := c.Get(zkChroot + "/" + frameworkName + "_reconciliation")
	if err == zk.ErrNoNode {
		return map[string]string{}, nil
	}
	if err != nil {
		return map[string]string{}, err
	}
	reconciliationInfo := map[string]string{}
	err = json.Unmarshal(rawData, &reconciliationInfo)
	return reconciliationInfo, err
}

func ClearZKState(
	zkServers []string,
	zkChroot string,
	frameworkName string,
) error {
	c, _, err := zk.Connect(zkServers, RPC_TIMEOUT)
	if err != nil {
		return err
	}
	defer c.Close()
	err1 := c.Delete(zkChroot+"/"+frameworkName+"_framework_id", -1)
	err2 := c.Delete(zkChroot+"/"+frameworkName+"_reconciliation", -1)
	if err1 != nil {
		return err1
	} else if err2 != nil {
		return err2
	} else {
		return nil
	}
}

func GetMasterFromZK(zkURI string) (string, error) {
	servers, chroot, err := ParseZKURI(zkURI)
	c, _, err := zk.Connect(servers, RPC_TIMEOUT)
	if err != nil {
		return "", err
	}
	defer c.Close()

	children, _, err := c.Children(chroot)
	if err != nil {
		return "", err
	}

	lowest := ""
	ss := sort.StringSlice(children)
	ss.Sort()
	for i := 0; i < len(ss); i++ {
		if strings.HasPrefix(ss[i], "info_") {
			lowest = ss[i]
			break
		}
	}
	if lowest == "" {
		return "", errors.New("Could not find current mesos master in zk")
	}
	rawData, _, err := c.Get(chroot + "/" + lowest)
	mraw := strings.Split(string(rawData), "@")[1]
	mraw2 := strings.Split(mraw, ":")
	host := mraw2[0]
	port := uint16(0)
	_, err = fmt.Sscanf(mraw2[1], "%d", &port)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", host, port), nil
}
