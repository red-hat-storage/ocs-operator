// Copyright (c) 2023 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

<<<<<<<< HEAD:vendor/go.uber.org/zap/zapcore/lazy_with.go
package zapcore

import "sync"

type lazyWithCore struct {
	Core
	sync.Once
	fields []Field
}

// NewLazyWith wraps a Core with a "lazy" Core that will only encode fields if
// the logger is written to (or is further chained in a lon-lazy manner).
func NewLazyWith(core Core, fields []Field) Core {
	return &lazyWithCore{
		Core:   core,
		fields: fields,
	}
}

func (d *lazyWithCore) initOnce() {
	d.Once.Do(func() {
		d.Core = d.Core.With(d.fields)
	})
}

func (d *lazyWithCore) With(fields []Field) Core {
	d.initOnce()
	return d.Core.With(fields)
}

func (d *lazyWithCore) Check(e Entry, ce *CheckedEntry) *CheckedEntry {
	d.initOnce()
	return d.Core.Check(e, ce)
========
// Package pool provides internal pool utilities.
package pool

import (
	"sync"
)

// A Pool is a generic wrapper around [sync.Pool] to provide strongly-typed
// object pooling.
//
// Note that SA6002 (ref: https://staticcheck.io/docs/checks/#SA6002) will
// not be detected, so all internal pool use must take care to only store
// pointer types.
type Pool[T any] struct {
	pool sync.Pool
}

// New returns a new [Pool] for T, and will use fn to construct new Ts when
// the pool is empty.
func New[T any](fn func() T) *Pool[T] {
	return &Pool[T]{
		pool: sync.Pool{
			New: func() any {
				return fn()
			},
		},
	}
}

// Get gets a T from the pool, or creates a new one if the pool is empty.
func (p *Pool[T]) Get() T {
	return p.pool.Get().(T)
}

// Put returns x into the pool.
func (p *Pool[T]) Put(x T) {
	p.pool.Put(x)
>>>>>>>> 4f21ab4c (Update rook package version to latest of release-1.12 branch):vendor/go.uber.org/zap/internal/pool/pool.go
}
