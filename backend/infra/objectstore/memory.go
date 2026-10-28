package objectstore

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"

	domain "github.com/guide-flow/guide-flow/backend/domain/lesson"
)

var ErrObjectNotFound = errors.New("object not found")

type MemoryImageStore struct {
	mu     sync.RWMutex
	images map[string][]byte
}

func NewMemoryImageStore() *MemoryImageStore {
	return &MemoryImageStore{images: map[string][]byte{}}
}

func (s *MemoryImageStore) Put(id string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.images[id] = append([]byte(nil), data...)
}

func (s *MemoryImageStore) Save(_ context.Context, id string, source io.Reader) error {
	data, err := io.ReadAll(source)
	if err != nil {
		return err
	}
	s.Put(id, data)
	return nil
}

func (s *MemoryImageStore) Open(_ context.Context, id string) (io.ReadCloser, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, ok := s.images[id]
	if !ok {
		return nil, ErrObjectNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (s *MemoryImageStore) Delete(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.images, id)
	return nil
}

func (s *MemoryImageStore) References(_ context.Context, id string) (domain.ImageReferences, error) {
	return domain.ImageReferences{
		OriginalObjectKey:  "memory://lessons/" + id + "/original",
		ThumbnailObjectKey: "memory://lessons/" + id + "/thumbnail",
	}, nil
}
