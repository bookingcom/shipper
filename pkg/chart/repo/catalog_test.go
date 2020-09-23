package repo

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
)

type TestCache struct {
	name  string
	cache map[string][]byte
	*sync.Mutex
}

var _ Cache = (*TestCache)(nil)

func NewTestCache(name string) *TestCache {
	return &TestCache{
		name,
		make(map[string][]byte),
		&sync.Mutex{},
	}
}

func (tc *TestCache) Fetch(name string) ([]byte, error) {
	tc.Lock()
	defer tc.Unlock()

	if v, ok := tc.cache[name]; ok {
		return v, nil
	}

	return nil, os.ErrNotExist
}

func (tc *TestCache) Store(name string, data []byte) error {
	tc.Lock()
	defer tc.Unlock()

	tc.cache[name] = data

	return nil
}

func (tc *TestCache) Clean() error {
	tc.Lock()
	defer tc.Unlock()

	tc.cache = make(map[string][]byte)

	return nil
}

func TestCreateRepoIfNotExist(t *testing.T) {

	testCacheFactory := func(name string) (Cache, error) {
		return NewTestCache(name), nil
	}

	tests := []struct {
		name    string
		url     string
		err     error
		factory CacheFactory
	}{
		{
			name:    "valid URL",
			url:     "https://charts.example.com",
			err:     nil,
			factory: testCacheFactory,
		},
		{
			name:    "invalid URL",
			url:     "an invalid url string",
			err:     fmt.Errorf("internal chart repo client error"),
			factory: testCacheFactory,
		},
	}

	t.Parallel()

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			stopCh := make(chan struct{})
			defer close(stopCh)
			c := NewCatalog(testCase.factory, func(_ string) ([]byte, error) {
				return []byte{}, nil
			}, stopCh)
			_, err := c.CreateRepoIfNotExist(testCase.url)
			if (err == nil && testCase.err != nil) ||
				(err != nil && testCase.err == nil) ||
				(err != nil && !strings.Contains(err.Error(), testCase.err.Error())) {
				t.Fatalf("Unexpected error on calling NewCatalog(): got: %q, want: %q", err, testCase.err)
			}
		})
	}
}
