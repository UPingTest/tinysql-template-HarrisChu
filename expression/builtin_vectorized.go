// Copyright 2019 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package expression

import (
	"sync"

	"github.com/pingcap/errors"
	"github.com/pingcap/tidb/parser/mysql"
	"github.com/pingcap/tidb/types"
	"github.com/pingcap/tidb/util/chunk"
)

// columnBufferAllocator is used to allocate and release column buffer in vectorized evaluation.
type columnBufferAllocator interface {
	// get allocates a column buffer with the specific eval type and capacity.
	// the allocator is not responsible for initializing the column, so please initialize it before using.
	get(evalType types.EvalType, capacity int) (*chunk.Column, error)
	// put releases a column buffer.
	put(buf *chunk.Column)
}

// localSliceBuffer implements columnBufferAllocator interface.
// It works like a concurrency-safe deque which is implemented by a lock + slice.
type localSliceBuffer struct {
	sync.Mutex
	buffers []*chunk.Column
	head    int
	tail    int
	size    int
}

func newLocalSliceBuffer(initCap int) *localSliceBuffer {
	return &localSliceBuffer{buffers: make([]*chunk.Column, initCap)}
}

var globalColumnAllocator = newLocalSliceBuffer(1024)

func newBuffer(evalType types.EvalType, capacity int) (*chunk.Column, error) {
	switch evalType {
	case types.ETInt:
		return chunk.NewColumn(types.NewFieldType(mysql.TypeLonglong), capacity), nil
	case types.ETReal:
		return chunk.NewColumn(types.NewFieldType(mysql.TypeDouble), capacity), nil
	case types.ETString:
		return chunk.NewColumn(types.NewFieldType(mysql.TypeString), capacity), nil
	}
	return nil, errors.Errorf("get column buffer for unsupported EvalType=%v", evalType)
}

// GetColumn allocates a column buffer with the specific eval type and capacity.
// the allocator is not responsible for initializing the column, so please initialize it before using.
func GetColumn(evalType types.EvalType, capacity int) (*chunk.Column, error) {
	return globalColumnAllocator.get(evalType, capacity)
}

// PutColumn releases a column buffer.
func PutColumn(buf *chunk.Column) {
	globalColumnAllocator.put(buf)
}

func (r *localSliceBuffer) get(evalType types.EvalType, capacity int) (*chunk.Column, error) {
	r.Lock()
	if r.size > 0 {
		buf := r.buffers[r.head]
		r.head++
		if r.head == len(r.buffers) {
			r.head = 0
		}
		r.size--
		r.Unlock()
		return buf, nil
	}
	r.Unlock()
	return newBuffer(evalType, capacity)
}

func (r *localSliceBuffer) put(buf *chunk.Column) {
	r.Lock()
	if r.size == len(r.buffers) {
		buffers := make([]*chunk.Column, len(r.buffers)*2)
		copy(buffers, r.buffers[r.head:])
		copy(buffers[r.size-r.head:], r.buffers[:r.tail])
		r.head = 0
		r.tail = len(r.buffers)
		r.buffers = buffers
	}
	r.buffers[r.tail] = buf
	r.tail++
	if r.tail == len(r.buffers) {
		r.tail = 0
	}
	r.size++
	r.Unlock()
}
