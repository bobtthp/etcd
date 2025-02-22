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
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/anishathalye/porcupine"
)

type OperationType string

const (
	Get    OperationType = "get"
	Put    OperationType = "put"
	Delete OperationType = "delete"
	Txn    OperationType = "txn"
)

type EtcdRequest struct {
	Conds []EtcdCondition
	Ops   []EtcdOperation
}

type EtcdCondition struct {
	Key           string
	ExpectedValue string
}

type EtcdOperation struct {
	Type  OperationType
	Key   string
	Value string
}

type EtcdResponse struct {
	Err        error
	Revision   int64
	TxnFailure bool
	Result     []EtcdOperationResult
}

type EtcdOperationResult struct {
	Value   string
	Deleted int64
}

type PossibleStates []EtcdState

type EtcdState struct {
	Revision  int64
	KeyValues map[string]string
}

var etcdModel = porcupine.Model{
	Init: func() interface{} {
		return "[]" // empty PossibleStates
	},
	Step: func(st interface{}, in interface{}, out interface{}) (bool, interface{}) {
		var states PossibleStates
		err := json.Unmarshal([]byte(st.(string)), &states)
		if err != nil {
			panic(err)
		}
		ok, states := step(states, in.(EtcdRequest), out.(EtcdResponse))
		data, err := json.Marshal(states)
		if err != nil {
			panic(err)
		}
		return ok, string(data)
	},
	DescribeOperation: func(in, out interface{}) string {
		return describeEtcdRequestResponse(in.(EtcdRequest), out.(EtcdResponse))
	},
}

func describeEtcdRequestResponse(request EtcdRequest, response EtcdResponse) string {
	prefix := describeEtcdOperations(request.Ops)
	if len(request.Conds) != 0 {
		prefix = fmt.Sprintf("if(%s).then(%s)", describeEtcdConditions(request.Conds), prefix)
	}

	return fmt.Sprintf("%s -> %s", prefix, describeEtcdResponse(request.Ops, response))
}

func describeEtcdConditions(conds []EtcdCondition) string {
	opsDescription := make([]string, len(conds))
	for i := range conds {
		opsDescription[i] = fmt.Sprintf("%s==%q", conds[i].Key, conds[i].ExpectedValue)
	}
	return strings.Join(opsDescription, " && ")
}

func describeEtcdOperations(ops []EtcdOperation) string {
	opsDescription := make([]string, len(ops))
	for i := range ops {
		opsDescription[i] = describeEtcdOperation(ops[i])
	}
	return strings.Join(opsDescription, ", ")
}

func describeEtcdResponse(ops []EtcdOperation, response EtcdResponse) string {
	if response.Err != nil {
		return fmt.Sprintf("err: %q", response.Err)
	}
	if response.TxnFailure {
		return fmt.Sprintf("txn failed, rev: %d", response.Revision)
	}
	respDescription := make([]string, len(response.Result))
	for i := range response.Result {
		respDescription[i] = describeEtcdOperationResponse(ops[i].Type, response.Result[i])
	}
	respDescription = append(respDescription, fmt.Sprintf("rev: %d", response.Revision))
	return strings.Join(respDescription, ", ")
}

func describeEtcdOperation(op EtcdOperation) string {
	switch op.Type {
	case Get:
		return fmt.Sprintf("get(%q)", op.Key)
	case Put:
		return fmt.Sprintf("put(%q, %q)", op.Key, op.Value)
	case Delete:
		return fmt.Sprintf("delete(%q)", op.Key)
	case Txn:
		return "<! unsupported: nested transaction !>"
	default:
		return fmt.Sprintf("<! unknown op: %q !>", op.Type)
	}
}

func describeEtcdOperationResponse(op OperationType, resp EtcdOperationResult) string {
	switch op {
	case Get:
		if resp.Value == "" {
			return "nil"
		}
		return fmt.Sprintf("%q", resp.Value)
	case Put:
		return fmt.Sprintf("ok")
	case Delete:
		return fmt.Sprintf("deleted: %d", resp.Deleted)
	case Txn:
		return "<! unsupported: nested transaction !>"
	default:
		return fmt.Sprintf("<! unknown op: %q !>", op)
	}
}

func step(states PossibleStates, request EtcdRequest, response EtcdResponse) (bool, PossibleStates) {
	if len(states) == 0 {
		// states were not initialized
		if response.Err != nil {
			return true, nil
		}
		return true, PossibleStates{initState(request, response)}
	}
	if response.Err != nil {
		states = applyFailedRequest(states, request)
	} else {
		states = applyRequest(states, request, response)
	}
	return len(states) > 0, states
}

// initState tries to create etcd state based on the first request.
func initState(request EtcdRequest, response EtcdResponse) EtcdState {
	state := EtcdState{
		Revision:  response.Revision,
		KeyValues: map[string]string{},
	}
	if response.TxnFailure {
		return state
	}
	for i, op := range request.Ops {
		opResp := response.Result[i]
		switch op.Type {
		case Get:
			if opResp.Value != "" {
				state.KeyValues[op.Key] = opResp.Value
			}
		case Put:
			state.KeyValues[op.Key] = op.Value
		case Delete:
		default:
			panic("Unknown operation")
		}
	}
	return state
}

// applyFailedRequest handles a failed requests, one that it's not known if it was persisted or not.
func applyFailedRequest(states PossibleStates, request EtcdRequest) PossibleStates {
	for _, s := range states {
		newState, _ := applyRequestToSingleState(s, request)
		states = append(states, newState)
	}
	return states
}

// applyRequest handles a successful request by applying it to possible states and checking if they match the response.
func applyRequest(states PossibleStates, request EtcdRequest, response EtcdResponse) PossibleStates {
	newStates := make(PossibleStates, 0, len(states))
	for _, s := range states {
		newState, expectResponse := applyRequestToSingleState(s, request)
		if reflect.DeepEqual(expectResponse, response) {
			newStates = append(newStates, newState)
		}
	}
	return newStates
}

// applyRequestToSingleState handles a successful request, returning updated state and response it would generate.
func applyRequestToSingleState(s EtcdState, request EtcdRequest) (EtcdState, EtcdResponse) {
	success := true
	for _, cond := range request.Conds {
		if val := s.KeyValues[cond.Key]; val != cond.ExpectedValue {
			success = false
			break
		}
	}
	if !success {
		return s, EtcdResponse{Revision: s.Revision, TxnFailure: true}
	}
	newKVs := map[string]string{}
	for k, v := range s.KeyValues {
		newKVs[k] = v
	}
	s.KeyValues = newKVs
	opResp := make([]EtcdOperationResult, len(request.Ops))
	increaseRevision := false
	for i, op := range request.Ops {
		switch op.Type {
		case Get:
			opResp[i].Value = s.KeyValues[op.Key]
		case Put:
			s.KeyValues[op.Key] = op.Value
			increaseRevision = true
		case Delete:
			if _, ok := s.KeyValues[op.Key]; ok {
				delete(s.KeyValues, op.Key)
				increaseRevision = true
				opResp[i].Deleted = 1
			}
		default:
			panic("unsupported operation")
		}
	}
	if increaseRevision {
		s.Revision += 1
	}
	return s, EtcdResponse{Result: opResp, Revision: s.Revision}
}
