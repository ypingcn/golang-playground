// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"sync"
)

type store interface {
	PutSnippet(ctx context.Context, id string, snip *snippet) error
	GetSnippet(ctx context.Context, id string, snip *snippet) error
}

// inMemStore is a store backed by a map that should only be used for testing.
type inMemStore struct {
	sync.RWMutex
	m map[string]*snippet // key -> snippet
}

func (s *inMemStore) PutSnippet(_ context.Context, id string, snip *snippet) error {
	s.Lock()
	if s.m == nil {
		s.m = map[string]*snippet{}
	}
	b := make([]byte, len(snip.Body))
	copy(b, snip.Body)
	s.m[id] = &snippet{Body: b}
	s.Unlock()
	return nil
}

func (s *inMemStore) GetSnippet(_ context.Context, id string, snip *snippet) error {
	var ErrNoSuchEntity = errors.New("datastore: no such entity")

	s.RLock()
	defer s.RUnlock()
	v, ok := s.m[id]
	if !ok {
		return ErrNoSuchEntity
	}
	*snip = *v
	return nil
}
