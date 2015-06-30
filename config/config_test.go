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
	"reflect"
	"testing"
)

func TestNode_Parse(t *testing.T) {
	for i, tt := range []struct {
		text string
		want *Node
		err  error
	}{
		{"", nil, ErrUnmarshal},
		{" ", nil, ErrUnmarshal},
		{"a", nil, ErrUnmarshal},
		{"a b", nil, ErrUnmarshal},
		{"a b c", nil, ErrUnmarshal},
		{"a b 1 2 3", nil, ErrUnmarshal},
		{"a b c d", nil, ErrUnmarshal},
		{"a b c 1", nil, ErrUnmarshal},
		{"a b 1 d", nil, ErrUnmarshal},
		{"a b 1 2", &Node{Name: "a", Host: "b", RPCPort: 1, ClientPort: 2}, nil},
	} {
		if n, err := Parse(tt.text); !reflect.DeepEqual(err, tt.err) {
			t.Errorf("test #%d: got err: %v, want: %v", i, err, tt.err)
		} else if got := n; !reflect.DeepEqual(got, tt.want) {
			t.Errorf("test #%d: got: %v, want: %v", i, got, tt.want)
		}
	}
}

func TestNode_String(t *testing.T) {
	for i, tt := range []struct {
		Node
		want string
	}{
		{Node{}, "  0 0"},
		{Node{Name: "a"}, "a  0 0"},
		{Node{Host: "b"}, " b 0 0"},
		{Node{RPCPort: 1}, "  1 0"},
		{Node{ClientPort: 1}, "  0 1"},
		{Node{Name: "a", Host: "b", RPCPort: 1, ClientPort: 2}, "a b 1 2"},
	} {
		if got := tt.String(); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("test #%d: got : %s, want: %s", i, got, tt.want)
		}
	}
}
