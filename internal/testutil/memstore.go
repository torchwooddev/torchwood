package testutil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	domainstorage "github.com/torchwooddev/torchwood/internal/domain/storage"
)

// MemObjectStore is an in-memory ObjectStore for integration tests.
type MemObjectStore struct {
	mu      sync.Mutex
	buckets map[string]map[string][]byte
}

func NewMemObjectStore() *MemObjectStore {
	return &MemObjectStore{buckets: map[string]map[string][]byte{}}
}

func (m *MemObjectStore) EnsureBucket(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.buckets[name]; !ok {
		m.buckets[name] = map[string][]byte{}
	}
	return nil
}

func (m *MemObjectStore) Put(_ context.Context, bucket, key string, data io.Reader, size int64, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.buckets[bucket]; !ok {
		m.buckets[bucket] = map[string][]byte{}
	}
	buf := make([]byte, size)
	if _, err := io.ReadFull(data, buf); err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return err
	}
	m.buckets[bucket][key] = bytes.Clone(buf)
	return nil
}

func (m *MemObjectStore) Get(_ context.Context, bucket, key string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	objects, ok := m.buckets[bucket]
	if !ok {
		return nil, fmt.Errorf("object not found: %s/%s", bucket, key)
	}
	data, ok := objects[key]
	if !ok {
		return nil, fmt.Errorf("object not found: %s/%s", bucket, key)
	}
	return io.NopCloser(bytes.NewReader(bytes.Clone(data))), nil
}

func (m *MemObjectStore) Delete(_ context.Context, bucket, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if objects, ok := m.buckets[bucket]; ok {
		delete(objects, key)
	}
	return nil
}

var _ domainstorage.ObjectStore = (*MemObjectStore)(nil)
