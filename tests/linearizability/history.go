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
	"time"

	"github.com/anishathalye/porcupine"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type appendableHistory struct {
	// id of the next write operation. If needed a new id might be requested from idProvider.
	id         int
	idProvider idProvider

	history
}

func newAppendableHistory(ids idProvider) *appendableHistory {
	return &appendableHistory{
		id:         ids.ClientId(),
		idProvider: ids,
		history: history{
			successful: []porcupine.Operation{},
			failed:     []porcupine.Operation{},
		},
	}
}

func (h *appendableHistory) AppendGet(key string, start, end time.Time, resp *clientv3.GetResponse) {
	var readData string
	if len(resp.Kvs) == 1 {
		readData = string(resp.Kvs[0].Value)
	}
	var revision int64
	if resp != nil && resp.Header != nil {
		revision = resp.Header.Revision
	}
	h.successful = append(h.successful, porcupine.Operation{
		ClientId: h.id,
		Input:    getRequest(key),
		Call:     start.UnixNano(),
		Output:   getResponse(readData, revision),
		Return:   end.UnixNano(),
	})
}

func (h *appendableHistory) AppendPut(key, value string, start, end time.Time, resp *clientv3.PutResponse, err error) {
	request := putRequest(key, value)
	if err != nil {
		h.appendFailed(request, start, err)
		return
	}
	var revision int64
	if resp != nil && resp.Header != nil {
		revision = resp.Header.Revision
	}
	h.successful = append(h.successful, porcupine.Operation{
		ClientId: h.id,
		Input:    request,
		Call:     start.UnixNano(),
		Output:   putResponse(revision),
		Return:   end.UnixNano(),
	})
}

func (h *appendableHistory) AppendDelete(key string, start, end time.Time, resp *clientv3.DeleteResponse, err error) {
	request := deleteRequest(key)
	if err != nil {
		h.appendFailed(request, start, err)
		return
	}
	var revision int64
	var deleted int64
	if resp != nil && resp.Header != nil {
		revision = resp.Header.Revision
		deleted = resp.Deleted
	}
	h.successful = append(h.successful, porcupine.Operation{
		ClientId: h.id,
		Input:    request,
		Call:     start.UnixNano(),
		Output:   deleteResponse(deleted, revision),
		Return:   end.UnixNano(),
	})
}

func (h *appendableHistory) AppendTxn(key, expectValue, newValue string, start, end time.Time, resp *clientv3.TxnResponse, err error) {
	request := txnRequest(key, expectValue, newValue)
	if err != nil {
		h.appendFailed(request, start, err)
		return
	}
	var revision int64
	if resp != nil && resp.Header != nil {
		revision = resp.Header.Revision
	}
	h.successful = append(h.successful, porcupine.Operation{
		ClientId: h.id,
		Input:    request,
		Call:     start.UnixNano(),
		Output:   txnResponse(resp.Succeeded, revision),
		Return:   end.UnixNano(),
	})
}

func (h *appendableHistory) appendFailed(request EtcdRequest, start time.Time, err error) {
	h.failed = append(h.failed, porcupine.Operation{
		ClientId: h.id,
		Input:    request,
		Call:     start.UnixNano(),
		Output:   failedResponse(err),
		Return:   0, // For failed writes we don't know when request has really finished.
	})
	// Operations of single client needs to be sequential.
	// As we don't know return time of failed operations, all new writes need to be done with new client id.
	h.id = h.idProvider.ClientId()
}

func getRequest(key string) EtcdRequest {
	return EtcdRequest{Ops: []EtcdOperation{{Type: Get, Key: key}}}
}

func getResponse(value string, revision int64) EtcdResponse {
	return EtcdResponse{Result: []EtcdOperationResult{{Value: value}}, Revision: revision}
}

func failedResponse(err error) EtcdResponse {
	return EtcdResponse{Err: err}
}

func putRequest(key, value string) EtcdRequest {
	return EtcdRequest{Ops: []EtcdOperation{{Type: Put, Key: key, Value: value}}}
}

func putResponse(revision int64) EtcdResponse {
	return EtcdResponse{Result: []EtcdOperationResult{{}}, Revision: revision}
}

func deleteRequest(key string) EtcdRequest {
	return EtcdRequest{Ops: []EtcdOperation{{Type: Delete, Key: key}}}
}

func deleteResponse(deleted int64, revision int64) EtcdResponse {
	return EtcdResponse{Result: []EtcdOperationResult{{Deleted: deleted}}, Revision: revision}
}

func txnRequest(key, expectValue, newValue string) EtcdRequest {
	return EtcdRequest{Conds: []EtcdCondition{{Key: key, ExpectedValue: expectValue}}, Ops: []EtcdOperation{{Type: Put, Key: key, Value: newValue}}}
}

func txnResponse(succeeded bool, revision int64) EtcdResponse {
	var result []EtcdOperationResult
	if succeeded {
		result = []EtcdOperationResult{{}}
	}
	return EtcdResponse{Result: result, TxnFailure: !succeeded, Revision: revision}
}

type history struct {
	successful []porcupine.Operation
	// failed requests are kept separate as we don't know return time of failed operations.
	// Based on https://github.com/anishathalye/porcupine/issues/10
	failed []porcupine.Operation
}

func (h history) Merge(h2 history) history {
	result := history{
		successful: make([]porcupine.Operation, 0, len(h.successful)+len(h2.successful)),
		failed:     make([]porcupine.Operation, 0, len(h.failed)+len(h2.failed)),
	}
	result.successful = append(result.successful, h.successful...)
	result.successful = append(result.successful, h2.successful...)
	result.failed = append(result.failed, h.failed...)
	result.failed = append(result.failed, h2.failed...)
	return result
}

func (h history) Operations() []porcupine.Operation {
	operations := make([]porcupine.Operation, 0, len(h.successful)+len(h.failed))
	var maxTime int64
	for _, op := range h.successful {
		operations = append(operations, op)
		if op.Return > maxTime {
			maxTime = op.Return
		}
	}
	// Failed requests don't have a known return time.
	// We simulate Infinity by using return time of latest successfully request.
	for _, op := range h.failed {
		if op.Call > maxTime {
			continue
		}
		op.Return = maxTime + 1
		operations = append(operations, op)
	}
	return operations
}
