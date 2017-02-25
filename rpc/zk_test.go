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
	"reflect"
	"testing"

	"github.com/gogo/protobuf/proto"
	mesos "github.com/mesos/mesos-go/api/v0/mesosproto"
)

func TestAddressFrom(t *testing.T) {
	for i, tc := range []struct {
		info         *mesos.MasterInfo
		wantsAddress string
	}{
		{nil, ""},
		{&mesos.MasterInfo{}, ""},
		{&mesos.MasterInfo{Port: proto.Uint32(2)}, ""},
		{&mesos.MasterInfo{Ip: proto.Uint32(0x01010101), Port: proto.Uint32(2)}, "1.1.1.1:2"},
		{&mesos.MasterInfo{Hostname: proto.String("foo"), Port: proto.Uint32(2)}, "foo:2"},
		{&mesos.MasterInfo{Hostname: proto.String("foo"), Ip: proto.Uint32(0x01010101), Port: proto.Uint32(2)}, "foo:2"},
		{&mesos.MasterInfo{Address: &mesos.Address{}}, ""},
		{&mesos.MasterInfo{Address: &mesos.Address{Hostname: proto.String("fab")}}, "fab:0"},
		{&mesos.MasterInfo{Address: &mesos.Address{Ip: proto.String("fab")}}, "fab:0"},
		{&mesos.MasterInfo{Address: &mesos.Address{Port: proto.Int32(2)}}, ""},
		{&mesos.MasterInfo{Address: &mesos.Address{Hostname: proto.String("fab"), Port: proto.Int32(2)}}, "fab:2"},
		{&mesos.MasterInfo{Address: &mesos.Address{Ip: proto.String("fab"), Port: proto.Int32(2)}}, "fab:2"},
		{&mesos.MasterInfo{Address: &mesos.Address{Hostname: proto.String("fab"), Ip: proto.String("baz"), Port: proto.Int32(2)}}, "fab:2"},
		{&mesos.MasterInfo{
			Hostname: proto.String("kok"), Ip: proto.Uint32(0x01010101), Port: proto.Uint32(3),
			Address: &mesos.Address{Hostname: proto.String("fab"), Ip: proto.String("baz"), Port: proto.Int32(2)}}, "fab:2"},
		{&mesos.MasterInfo{
			Hostname: proto.String("kok"), Ip: proto.Uint32(0x01010101), Port: proto.Uint32(3),
			Address: &mesos.Address{Ip: proto.String("baz"), Port: proto.Int32(2)}}, "baz:2"},
		{&mesos.MasterInfo{
			Hostname: proto.String("kok"), Ip: proto.Uint32(0x01010101), Port: proto.Uint32(3),
			Address: &mesos.Address{Port: proto.Int32(2)}}, "kok:3"},
	} {
		address := addressFrom(tc.info)
		if address != tc.wantsAddress {
			t.Errorf("test case %d failed, expected %q instead of %q", i, tc.wantsAddress, address)
		}
	}
}

func TestMasterInfoFromZKNodes(t *testing.T) {
	mkProto := func(jsonInfo string) []byte {
		var info mesos.MasterInfo
		err := json.Unmarshal(([]byte)(jsonInfo), &info)
		if err != nil {
			t.Fatal(err)
		} else {
			data, err := proto.Marshal(&info)
			if err != nil {
				t.Fatal(err)
			} else {
				return data
			}
		}
		return nil
	}
	const (
		jsonInfo2 = `{"id":"info2", "ip": 33686018, "port": 5050, "address":{"hostname":"foo", "port": 1}}`
		jsonInfo3 = `{"id":"info3", "ip": 50529027, "port": 5050, "address":{"hostname":"bar", "port": 2}}`
	)
	var (
		unknownNodeErr = errors.New("unknown node")
		info2          = mkProto(jsonInfo2)
		info3          = mkProto(jsonInfo3)
		gFixture       = nodeGetter(func(node string) ([]byte, error) {
			switch node {
			case "json.info_001":
				return []byte(`{}`), nil
			case "info_002":
				return info2, nil
			case "info_003":
				return info3, nil
			case "json.info_002":
				return []byte(jsonInfo2), nil
			case "json.info_003":
				return []byte(jsonInfo3), nil
			default:
				return nil, unknownNodeErr
			}
		})
	)
	for i, tc := range []struct {
		children     []string
		getter       nodeGetter
		codecs       map[string]decoder
		wantsInfo    *mesos.MasterInfo
		wantsRawData string
		wantsError   bool
	}{
		{wantsError: true},
		{children: []string{"a"}, wantsError: true},
		{children: []string{"json.info_001", "a"}, codecs: infoCodecs, getter: gFixture, wantsRawData: `{}`, wantsInfo: &mesos.MasterInfo{}},
		{children: []string{"json.info_002"}, codecs: infoCodecs, getter: gFixture, wantsRawData: jsonInfo2,
			wantsInfo: &mesos.MasterInfo{Id: proto.String("info2"), Ip: proto.Uint32(0x02020202), Port: proto.Uint32(5050),
				Address: &mesos.Address{Hostname: proto.String("foo"), Port: proto.Int32(1)}}},
		{children: []string{"json.info_003", "json.info_002"}, codecs: infoCodecs, getter: gFixture, wantsRawData: jsonInfo2,
			wantsInfo: &mesos.MasterInfo{Id: proto.String("info2"), Ip: proto.Uint32(0x02020202), Port: proto.Uint32(5050),
				Address: &mesos.Address{Hostname: proto.String("foo"), Port: proto.Int32(1)}}},
		{children: []string{"info_002"}, codecs: infoCodecs, getter: gFixture, wantsRawData: string(info2),
			wantsInfo: &mesos.MasterInfo{Id: proto.String("info2"), Ip: proto.Uint32(0x02020202), Port: proto.Uint32(5050),
				Address: &mesos.Address{Hostname: proto.String("foo"), Port: proto.Int32(1)}}},
		{children: []string{"info_003", "info_002"}, codecs: infoCodecs, getter: gFixture, wantsRawData: string(info2),
			wantsInfo: &mesos.MasterInfo{Id: proto.String("info2"), Ip: proto.Uint32(0x02020202), Port: proto.Uint32(5050),
				Address: &mesos.Address{Hostname: proto.String("foo"), Port: proto.Int32(1)}}},
		{children: []string{"info_003", "info_002", "json.info_003"}, codecs: infoCodecs, getter: gFixture, wantsRawData: string(info2),
			wantsInfo: &mesos.MasterInfo{Id: proto.String("info2"), Ip: proto.Uint32(0x02020202), Port: proto.Uint32(5050),
				Address: &mesos.Address{Hostname: proto.String("foo"), Port: proto.Int32(1)}}},
		{children: []string{"info_003", "info_002", "json.info_002"}, codecs: infoCodecs, getter: gFixture, wantsRawData: jsonInfo2,
			wantsInfo: &mesos.MasterInfo{Id: proto.String("info2"), Ip: proto.Uint32(0x02020202), Port: proto.Uint32(5050),
				Address: &mesos.Address{Hostname: proto.String("foo"), Port: proto.Int32(1)}}},
		{children: []string{"info_003", "json.info_002", "info_002"}, codecs: infoCodecs, getter: gFixture, wantsRawData: jsonInfo2,
			wantsInfo: &mesos.MasterInfo{Id: proto.String("info2"), Ip: proto.Uint32(0x02020202), Port: proto.Uint32(5050),
				Address: &mesos.Address{Hostname: proto.String("foo"), Port: proto.Int32(1)}}},
	} {
		info, rawData, err := masterInfoFromZKNodes(tc.children, tc.getter, tc.codecs)
		if err != nil && !tc.wantsError {
			t.Errorf("test case %d failed, unexpected error %v", i, err)
		} else if err == nil && tc.wantsError {
			t.Errorf("test case %d failed, expected error but got none", i)
		} else {
			if rawData != tc.wantsRawData {
				t.Errorf("test case %d failed, expected raw data %q instead of %q", i, tc.wantsRawData, rawData)
			}
			if !reflect.DeepEqual(info, tc.wantsInfo) {
				t.Errorf("test case %d failed, expected info %q instead of %q", i, tc.wantsInfo, info)
			}
		}
	}
}
