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
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/gogo/protobuf/proto"
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

		// try to update an existing node, which may fail if it
		// does not exist yet.
		_, err = c.Set(zkChroot+"/"+frameworkName+"_reconciliation",
			serializedReconciliationInfo,
			-1)
		if err != zk.ErrNoNode {
			return err
		}

		// attempt to create the node, as it does not exist
		_, err = c.Create(zkChroot+"/"+frameworkName+"_reconciliation",
			serializedReconciliationInfo,
			0,
			zk.WorldACL(zk.PermAll),
		)
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
) (fwid string, err error) {
	request := func() (string, error) {
		c, _, err := zk.Connect(zkServers, RPC_TIMEOUT)
		if err != nil {
			return "", err
		}
		defer c.Close()
		rawData, _, err := c.Get(zkChroot + "/" + frameworkName + "_framework_id")
		return string(rawData), err
	}

	backoff := 1
	for retries := 0; retries < RPC_RETRIES; retries++ {
		fwid, err = request()
		if err == nil {
			return fwid, err
		}
		time.Sleep(time.Duration(backoff) * time.Second)
		backoff = int(math.Min(float64(backoff<<1), 8))
	}
	return "", err
}

func GetPreviousReconciliationInfo(
	zkServers []string,
	zkChroot string,
	frameworkName string,
) (recon map[string]string, err error) {
	request := func() (map[string]string, error) {
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

	backoff := 1
	for retries := 0; retries < RPC_RETRIES; retries++ {
		recon, err = request()
		if err == nil {
			return recon, err
		}
		time.Sleep(time.Duration(backoff) * time.Second)
		backoff = int(math.Min(float64(backoff<<1), 8))
	}
	return recon, err
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

type decoder func([]byte, interface{}) error

var infoCodecs = map[string]decoder{
	"info_": func(b []byte, i interface{}) error {
		return proto.Unmarshal(b, i.(proto.Message))
	},
	"json.info_": json.Unmarshal,
}

type nodeGetter func(string) ([]byte, error)

func masterInfoFromZKNodes(children []string, ng nodeGetter, codecs map[string]decoder) (*mesos.MasterInfo, string, error) {
	type nodeDecoder struct {
		node    string
		decoder decoder
	}
	// for example: 001 -> {info_001,proto.Unmarshal} or 001 -> {json.info_001,json.Unmarshal}
	mapped := make(map[string]nodeDecoder, len(children))

	// process children deterministically, preferring json to protobuf
	sort.Sort(sort.Reverse(sort.StringSlice(children)))
childloop:
	for i := range children {
		for p := range codecs {
			if strings.HasPrefix(children[i], p) {
				key := children[i][len(p):]
				if _, found := mapped[key]; found {
					continue childloop
				}
				mapped[key] = nodeDecoder{children[i], codecs[p]}
				children[i] = key
				continue childloop
			}
		}
	}

	if len(mapped) == 0 {
		return nil, "", errors.New("Could not find current mesos master in zk")
	}

	sort.Sort(sort.StringSlice(children))
	var (
		info   mesos.MasterInfo
		lowest = children[0]
	)
	rawData, err := ng(mapped[lowest].node)
	if err == nil {
		err = mapped[lowest].decoder(rawData, &info)
	}
	return &info, string(rawData), err
}

// byteOrder is instantiated at package initialization time to the
// binary.ByteOrder of the running process.
// https://groups.google.com/d/msg/golang-nuts/zmh64YkqOV8/iJe-TrTTeREJ
var byteOrder = func() binary.ByteOrder {
	switch x := uint32(0x01020304); *(*byte)(unsafe.Pointer(&x)) {
	case 0x01:
		return binary.BigEndian
	case 0x04:
		return binary.LittleEndian
	}
	panic("unknown byte order")
}()

func addressFrom(info *mesos.MasterInfo) string {
	var (
		host string
		port int
	)
	if addr := info.GetAddress(); addr != nil {
		host = addr.GetHostname()
		if host == "" {
			host = addr.GetIp()
		}
		port = int(addr.GetPort())
	}
	if host == "" {
		host = info.GetHostname()
		if host == "" {
			if ipAsInt := info.GetIp(); ipAsInt != 0 {
				ip := make([]byte, 4)
				byteOrder.PutUint32(ip, ipAsInt)
				host = net.IP(ip).To4().String()
			}
		}
		port = int(info.GetPort())
	}
	if host == "" {
		return ""
	}
	return net.JoinHostPort(host, strconv.Itoa(port))
}

func addressFromRaw(rawData string) (string, error) {
	// scrape the contents of the raw buffer for anything that looks like a PID
	var (
		mraw   = strings.Split(string(rawData), "@")[1]
		mraw2  = strings.Split(mraw, ":")
		host   = mraw2[0]
		port   = 0
		_, err = fmt.Sscanf(mraw2[1], "%d", &port)
	)
	if err != nil {
		return "", err
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
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
	getter := func(node string) (rawData []byte, err error) {
		rawData, _, err = c.Get(chroot + "/" + node)
		return
	}
	info, rawData, err := masterInfoFromZKNodes(children, getter, infoCodecs)
	if err != nil {
		return "", err
	}
	addr := addressFrom(info)
	if addr == "" {
		addr, err = addressFromRaw(rawData)
	}
	return addr, err
}
