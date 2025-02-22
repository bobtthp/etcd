// Copyright 2022 The etcd Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package linearizability

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelStep(t *testing.T) {
	tcs := []struct {
		name       string
		operations []testOperation
	}{
		{
			name: "First Get can start from non-empty value and non-zero revision",
			operations: []testOperation{
				{req: getRequest("key"), resp: getResponse("", 42)},
			},
		},
		{
			name: "First Put can start from non-zero revision",
			operations: []testOperation{
				{req: putRequest("key", "1"), resp: putResponse(42)},
			},
		},
		{
			name: "First delete can start from non-zero revision",
			operations: []testOperation{
				{req: deleteRequest("key"), resp: deleteResponse(0, 42)},
			},
		},
		{
			name: "First Txn can start from non-zero revision",
			operations: []testOperation{
				{req: txnRequest("key", "", "42"), resp: txnResponse(false, 42)},
			},
		},
		{
			name: "Get response data should match put",
			operations: []testOperation{
				{req: putRequest("key1", "11"), resp: putResponse(1)},
				{req: putRequest("key2", "12"), resp: putResponse(2)},
				{req: getRequest("key1"), resp: getResponse("11", 1), failure: true},
				{req: getRequest("key1"), resp: getResponse("12", 1), failure: true},
				{req: getRequest("key1"), resp: getResponse("12", 2), failure: true},
				{req: getRequest("key1"), resp: getResponse("11", 2)},
				{req: getRequest("key2"), resp: getResponse("11", 2), failure: true},
				{req: getRequest("key2"), resp: getResponse("12", 1), failure: true},
				{req: getRequest("key2"), resp: getResponse("11", 1), failure: true},
				{req: getRequest("key2"), resp: getResponse("12", 2)},
			},
		},
		{
			name: "Put must increase revision by 1",
			operations: []testOperation{
				{req: getRequest("key"), resp: getResponse("", 1)},
				{req: putRequest("key", "1"), resp: putResponse(1), failure: true},
				{req: putRequest("key", "1"), resp: putResponse(3), failure: true},
				{req: putRequest("key", "1"), resp: putResponse(2)},
			},
		},
		{
			name: "Put can fail and be lost before get",
			operations: []testOperation{
				{req: putRequest("key", "1"), resp: putResponse(1)},
				{req: putRequest("key", "1"), resp: failedResponse(errors.New("failed"))},
				{req: getRequest("key"), resp: getResponse("1", 1)},
				{req: getRequest("key"), resp: getResponse("2", 1), failure: true},
				{req: getRequest("key"), resp: getResponse("1", 2), failure: true},
				{req: getRequest("key"), resp: getResponse("2", 2), failure: true},
			},
		},
		{
			name: "Put can fail and be lost before put",
			operations: []testOperation{
				{req: getRequest("key"), resp: getResponse("", 1)},
				{req: putRequest("key", "1"), resp: failedResponse(errors.New("failed"))},
				{req: putRequest("key", "3"), resp: getResponse("", 2)},
			},
		},
		{
			name: "Put can fail and be lost before delete",
			operations: []testOperation{
				{req: deleteRequest("key"), resp: deleteResponse(0, 1)},
				{req: putRequest("key", "1"), resp: failedResponse(errors.New("failed"))},
				{req: deleteRequest("key"), resp: deleteResponse(0, 1)},
			},
		},
		{
			name: "Put can fail and be lost before txn",
			operations: []testOperation{
				// Txn failure
				{req: getRequest("key"), resp: getResponse("", 1)},
				{req: putRequest("key", "1"), resp: failedResponse(errors.New("failed"))},
				{req: txnRequest("key", "2", "3"), resp: txnResponse(false, 1)},
				// Txn success
				{req: putRequest("key", "2"), resp: putResponse(2)},
				{req: putRequest("key", "4"), resp: failedResponse(errors.New("failed"))},
				{req: txnRequest("key", "2", "5"), resp: txnResponse(true, 3)},
			},
		},
		{
			name:       "Put can fail and be lost before txn success",
			operations: []testOperation{},
		},
		{
			name: "Put can fail but be persisted and increase revision before get",
			operations: []testOperation{
				// One failed request, one persisted.
				{req: putRequest("key", "1"), resp: putResponse(1)},
				{req: putRequest("key", "2"), resp: failedResponse(errors.New("failed"))},
				{req: getRequest("key"), resp: getResponse("3", 2), failure: true},
				{req: getRequest("key"), resp: getResponse("2", 1), failure: true},
				{req: getRequest("key"), resp: getResponse("2", 2)},
				// Two failed request, two persisted.
				{req: putRequest("key", "3"), resp: failedResponse(errors.New("failed"))},
				{req: putRequest("key", "4"), resp: failedResponse(errors.New("failed"))},
				{req: getRequest("key"), resp: getResponse("4", 4)},
			},
		},
		{
			name: "Put can fail but be persisted and increase revision before delete",
			operations: []testOperation{
				// One failed request, one persisted.
				{req: deleteRequest("key"), resp: deleteResponse(0, 1)},
				{req: putRequest("key", "1"), resp: failedResponse(errors.New("failed"))},
				{req: deleteRequest("key"), resp: deleteResponse(1, 1), failure: true},
				{req: deleteRequest("key"), resp: deleteResponse(1, 2), failure: true},
				{req: deleteRequest("key"), resp: deleteResponse(1, 3)},
				// Two failed request, two persisted.
				{req: putRequest("key", "4"), resp: putResponse(4)},
				{req: putRequest("key", "5"), resp: failedResponse(errors.New("failed"))},
				{req: putRequest("key", "6"), resp: failedResponse(errors.New("failed"))},
				{req: deleteRequest("key"), resp: deleteResponse(1, 7)},
				// Two failed request, one persisted.
				{req: putRequest("key", "8"), resp: putResponse(8)},
				{req: putRequest("key", "9"), resp: failedResponse(errors.New("failed"))},
				{req: putRequest("key", "10"), resp: failedResponse(errors.New("failed"))},
				{req: deleteRequest("key"), resp: deleteResponse(1, 10)},
			},
		},
		{
			name: "Put can fail but be persisted before txn",
			operations: []testOperation{
				// Txn success
				{req: getRequest("key"), resp: getResponse("", 1)},
				{req: putRequest("key", "2"), resp: failedResponse(errors.New("failed"))},
				{req: txnRequest("key", "2", ""), resp: txnResponse(true, 2), failure: true},
				{req: txnRequest("key", "2", ""), resp: txnResponse(true, 3)},
				// Txn failure
				{req: putRequest("key", "4"), resp: putResponse(4)},
				{req: txnRequest("key", "5", ""), resp: txnResponse(false, 4)},
				{req: putRequest("key", "5"), resp: failedResponse(errors.New("failed"))},
				{req: getRequest("key"), resp: getResponse("5", 5)},
			},
		},
		{
			name: "Delete only increases revision on success",
			operations: []testOperation{
				{req: putRequest("key1", "11"), resp: putResponse(1)},
				{req: putRequest("key2", "12"), resp: putResponse(2)},
				{req: deleteRequest("key1"), resp: deleteResponse(1, 2), failure: true},
				{req: deleteRequest("key1"), resp: deleteResponse(1, 3)},
				{req: deleteRequest("key1"), resp: deleteResponse(0, 4), failure: true},
				{req: deleteRequest("key1"), resp: deleteResponse(0, 3)},
			},
		},
		{
			name: "Delete not existing key",
			operations: []testOperation{
				{req: getRequest("key"), resp: getResponse("", 1)},
				{req: deleteRequest("key"), resp: deleteResponse(1, 2), failure: true},
				{req: deleteRequest("key"), resp: deleteResponse(0, 1)},
			},
		},
		{
			name: "Delete clears value",
			operations: []testOperation{
				{req: getRequest("key"), resp: getResponse("1", 1)},
				{req: deleteRequest("key"), resp: deleteResponse(1, 2)},
				{req: getRequest("key"), resp: getResponse("1", 1), failure: true},
				{req: getRequest("key"), resp: getResponse("1", 2), failure: true},
				{req: getRequest("key"), resp: getResponse("", 2)},
			},
		},
		{
			name: "Delete can fail and be lost before get",
			operations: []testOperation{
				{req: putRequest("key", "1"), resp: putResponse(1)},
				{req: deleteRequest("key"), resp: failedResponse(errors.New("failed"))},
				{req: getRequest("key"), resp: getResponse("1", 1)},
				{req: getRequest("key"), resp: getResponse("", 2), failure: true},
			},
		},
		{
			name: "Delete can fail and be lost before delete",
			operations: []testOperation{
				{req: putRequest("key", "1"), resp: putResponse(1)},
				{req: deleteRequest("key"), resp: failedResponse(errors.New("failed"))},
				{req: deleteRequest("key"), resp: deleteResponse(1, 1), failure: true},
				{req: deleteRequest("key"), resp: deleteResponse(1, 2)},
			},
		},
		{
			name: "Delete can fail and be lost before put",
			operations: []testOperation{
				{req: putRequest("key", "1"), resp: putResponse(1)},
				{req: deleteRequest("key"), resp: failedResponse(errors.New("failed"))},
				{req: putRequest("key", "1"), resp: putResponse(2)},
			},
		},
		{
			name: "Delete can fail but be persisted before get",
			operations: []testOperation{
				// One failed request, one persisted.
				{req: putRequest("key", "1"), resp: putResponse(1)},
				{req: deleteRequest("key"), resp: failedResponse(errors.New("failed"))},
				{req: getRequest("key"), resp: getResponse("", 2)},
				// Two failed request, one persisted.
				{req: putRequest("key", "3"), resp: putResponse(3)},
				{req: deleteRequest("key"), resp: failedResponse(errors.New("failed"))},
				{req: deleteRequest("key"), resp: failedResponse(errors.New("failed"))},
				{req: getRequest("key"), resp: getResponse("", 4)},
			},
		},
		{
			name: "Delete can fail but be persisted before put",
			operations: []testOperation{
				// One failed request, one persisted.
				{req: putRequest("key", "1"), resp: putResponse(1)},
				{req: deleteRequest("key"), resp: failedResponse(errors.New("failed"))},
				{req: putRequest("key", "3"), resp: putResponse(3)},
				// Two failed request, one persisted.
				{req: deleteRequest("key"), resp: failedResponse(errors.New("failed"))},
				{req: deleteRequest("key"), resp: failedResponse(errors.New("failed"))},
				{req: putRequest("key", "5"), resp: putResponse(5)},
			},
		},
		{
			name: "Delete can fail but be persisted before delete",
			operations: []testOperation{
				// One failed request, one persisted.
				{req: putRequest("key", "1"), resp: putResponse(1)},
				{req: deleteRequest("key"), resp: failedResponse(errors.New("failed"))},
				{req: deleteRequest("key"), resp: deleteResponse(0, 2)},
				{req: putRequest("key", "3"), resp: putResponse(3)},
				// Two failed request, one persisted.
				{req: deleteRequest("key"), resp: failedResponse(errors.New("failed"))},
				{req: deleteRequest("key"), resp: failedResponse(errors.New("failed"))},
				{req: deleteRequest("key"), resp: deleteResponse(0, 4)},
			},
		},
		{
			name: "Delete can fail but be persisted before txn",
			operations: []testOperation{
				// Txn success
				{req: getRequest("key"), resp: getResponse("1", 1)},
				{req: deleteRequest("key"), resp: failedResponse(errors.New("failed"))},
				{req: txnRequest("key", "", "3"), resp: txnResponse(true, 3)},
				// Txn failure
				{req: putRequest("key", "4"), resp: putResponse(4)},
				{req: deleteRequest("key"), resp: failedResponse(errors.New("failed"))},
				{req: txnRequest("key", "4", "5"), resp: txnResponse(false, 5)},
			},
		},
		{
			name: "Txn sets new value if value matches expected",
			operations: []testOperation{
				{req: getRequest("key"), resp: getResponse("1", 1)},
				{req: txnRequest("key", "1", "2"), resp: txnResponse(true, 1), failure: true},
				{req: txnRequest("key", "1", "2"), resp: txnResponse(false, 2), failure: true},
				{req: txnRequest("key", "1", "2"), resp: txnResponse(false, 1), failure: true},
				{req: txnRequest("key", "1", "2"), resp: txnResponse(true, 2)},
				{req: getRequest("key"), resp: getResponse("1", 1), failure: true},
				{req: getRequest("key"), resp: getResponse("1", 2), failure: true},
				{req: getRequest("key"), resp: getResponse("2", 1), failure: true},
				{req: getRequest("key"), resp: getResponse("2", 2)},
			},
		},
		{
			name: "Txn can expect on empty key",
			operations: []testOperation{
				{req: getRequest("key1"), resp: getResponse("", 1)},
				{req: txnRequest("key1", "", "2"), resp: txnResponse(true, 2)},
				{req: txnRequest("key2", "", "3"), resp: txnResponse(true, 3)},
				{req: txnRequest("key3", "4", "4"), resp: txnResponse(false, 4), failure: true},
			},
		},
		{
			name: "Txn doesn't do anything if value doesn't match expected",
			operations: []testOperation{
				{req: getRequest("key"), resp: getResponse("1", 1)},
				{req: txnRequest("key", "2", "3"), resp: txnResponse(true, 2), failure: true},
				{req: txnRequest("key", "2", "3"), resp: txnResponse(true, 1), failure: true},
				{req: txnRequest("key", "2", "3"), resp: txnResponse(false, 2), failure: true},
				{req: txnRequest("key", "2", "3"), resp: txnResponse(false, 1)},
				{req: getRequest("key"), resp: getResponse("2", 1), failure: true},
				{req: getRequest("key"), resp: getResponse("2", 2), failure: true},
				{req: getRequest("key"), resp: getResponse("3", 1), failure: true},
				{req: getRequest("key"), resp: getResponse("3", 2), failure: true},
				{req: getRequest("key"), resp: getResponse("1", 1)},
			},
		},
		{
			name: "Txn can fail and be lost before get",
			operations: []testOperation{
				{req: getRequest("key"), resp: getResponse("1", 1)},
				{req: txnRequest("key", "1", "2"), resp: failedResponse(errors.New("failed"))},
				{req: getRequest("key"), resp: getResponse("1", 1)},
				{req: getRequest("key"), resp: getResponse("2", 2), failure: true},
			},
		},
		{
			name: "Txn can fail and be lost before delete",
			operations: []testOperation{
				{req: getRequest("key"), resp: getResponse("1", 1)},
				{req: txnRequest("key", "1", "2"), resp: failedResponse(errors.New("failed"))},
				{req: deleteRequest("key"), resp: deleteResponse(1, 2)},
			},
		},
		{
			name: "Txn can fail and be lost before put",
			operations: []testOperation{
				{req: getRequest("key"), resp: getResponse("1", 1)},
				{req: txnRequest("key", "1", "2"), resp: failedResponse(errors.New("failed"))},
				{req: putRequest("key", "3"), resp: putResponse(2)},
			},
		},
		{
			name: "Txn can fail but be persisted before get",
			operations: []testOperation{
				// One failed request, one persisted.
				{req: getRequest("key"), resp: getResponse("1", 1)},
				{req: txnRequest("key", "1", "2"), resp: failedResponse(errors.New("failed"))},
				{req: getRequest("key"), resp: getResponse("2", 1), failure: true},
				{req: getRequest("key"), resp: getResponse("2", 2)},
				// Two failed request, two persisted.
				{req: putRequest("key", "3"), resp: putResponse(3)},
				{req: txnRequest("key", "3", "4"), resp: failedResponse(errors.New("failed"))},
				{req: txnRequest("key", "4", "5"), resp: failedResponse(errors.New("failed"))},
				{req: getRequest("key"), resp: getResponse("5", 5)},
			},
		},
		{
			name: "Txn can fail but be persisted before put",
			operations: []testOperation{
				// One failed request, one persisted.
				{req: getRequest("key"), resp: getResponse("1", 1)},
				{req: txnRequest("key", "1", "2"), resp: failedResponse(errors.New("failed"))},
				{req: putRequest("key", "3"), resp: putResponse(3)},
				// Two failed request, two persisted.
				{req: putRequest("key", "4"), resp: putResponse(4)},
				{req: txnRequest("key", "4", "5"), resp: failedResponse(errors.New("failed"))},
				{req: txnRequest("key", "5", "6"), resp: failedResponse(errors.New("failed"))},
				{req: putRequest("key", "7"), resp: putResponse(7)},
			},
		},
		{
			name: "Txn can fail but be persisted before delete",
			operations: []testOperation{
				// One failed request, one persisted.
				{req: getRequest("key"), resp: getResponse("1", 1)},
				{req: txnRequest("key", "1", "2"), resp: failedResponse(errors.New("failed"))},
				{req: deleteRequest("key"), resp: deleteResponse(1, 3)},
				// Two failed request, two persisted.
				{req: putRequest("key", "4"), resp: putResponse(4)},
				{req: txnRequest("key", "4", "5"), resp: failedResponse(errors.New("failed"))},
				{req: txnRequest("key", "5", "6"), resp: failedResponse(errors.New("failed"))},
				{req: deleteRequest("key"), resp: deleteResponse(1, 7)},
			},
		},
		{
			name: "Txn can fail but be persisted before txn",
			operations: []testOperation{
				// One failed request, one persisted with success.
				{req: getRequest("key"), resp: getResponse("1", 1)},
				{req: txnRequest("key", "1", "2"), resp: failedResponse(errors.New("failed"))},
				{req: txnRequest("key", "2", "3"), resp: txnResponse(true, 3)},
				// Two failed request, two persisted with success.
				{req: putRequest("key", "4"), resp: putResponse(4)},
				{req: txnRequest("key", "4", "5"), resp: failedResponse(errors.New("failed"))},
				{req: txnRequest("key", "5", "6"), resp: failedResponse(errors.New("failed"))},
				{req: txnRequest("key", "6", "7"), resp: txnResponse(true, 7)},
				// One failed request, one persisted with failure.
				{req: putRequest("key", "8"), resp: putResponse(8)},
				{req: txnRequest("key", "8", "9"), resp: failedResponse(errors.New("failed"))},
				{req: txnRequest("key", "8", "10"), resp: txnResponse(false, 9)},
			},
		},
	}
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			state := etcdModel.Init()
			for _, op := range tc.operations {
				ok, newState := etcdModel.Step(state, op.req, op.resp)
				if ok != !op.failure {
					t.Logf("state: %v", state)
					t.Errorf("Unexpected operation result, expect: %v, got: %v, operation: %s", !op.failure, ok, etcdModel.DescribeOperation(op.req, op.resp))
				}
				if ok {
					state = newState
					t.Logf("state: %v", state)
				}
			}
		})
	}
}

type testOperation struct {
	req     EtcdRequest
	resp    EtcdResponse
	failure bool
}

func TestModelDescribe(t *testing.T) {
	tcs := []struct {
		req            EtcdRequest
		resp           EtcdResponse
		expectDescribe string
	}{
		{
			req:            getRequest("key1"),
			resp:           getResponse("", 1),
			expectDescribe: `get("key1") -> nil, rev: 1`,
		},
		{
			req:            getRequest("key2"),
			resp:           getResponse("2", 2),
			expectDescribe: `get("key2") -> "2", rev: 2`,
		},
		{
			req:            putRequest("key3", "3"),
			resp:           putResponse(3),
			expectDescribe: `put("key3", "3") -> ok, rev: 3`,
		},
		{
			req:            putRequest("key4", "4"),
			resp:           failedResponse(errors.New("failed")),
			expectDescribe: `put("key4", "4") -> err: "failed"`,
		},
		{
			req:            deleteRequest("key5"),
			resp:           deleteResponse(1, 5),
			expectDescribe: `delete("key5") -> deleted: 1, rev: 5`,
		},
		{
			req:            deleteRequest("key6"),
			resp:           failedResponse(errors.New("failed")),
			expectDescribe: `delete("key6") -> err: "failed"`,
		},
		{
			req:            txnRequest("key7", "7", "77"),
			resp:           txnResponse(false, 7),
			expectDescribe: `if(key7=="7").then(put("key7", "77")) -> txn failed, rev: 7`,
		},
		{
			req:            txnRequest("key8", "8", "88"),
			resp:           txnResponse(true, 8),
			expectDescribe: `if(key8=="8").then(put("key8", "88")) -> ok, rev: 8`,
		},
		{
			req:            txnRequest("key9", "9", "99"),
			resp:           failedResponse(errors.New("failed")),
			expectDescribe: `if(key9=="9").then(put("key9", "99")) -> err: "failed"`,
		},
	}
	for _, tc := range tcs {
		assert.Equal(t, tc.expectDescribe, etcdModel.DescribeOperation(tc.req, tc.resp))
	}
}
