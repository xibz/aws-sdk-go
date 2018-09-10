package crr

import (
	"sync"
	"sync/atomic"
)

// EndpointCache is an LRU cache that holds a series of endpoints
// based on some key. The datastructure makes use of a read write
// mutex to enable asynchronous use.
type EndpointCache struct {
	endpoints     sync.Map
	endpointLimit int64
	// size is used to count the number elements in the cache.
	// The atomic package is used to ensure this size is accurate when
	// using multiple goroutines.
	size int64
}

// NewEndpointCache will return a newly initialized cache with a limit
// of endpointLimit entries.
func NewEndpointCache(endpointLimit int64) *EndpointCache {
	return &EndpointCache{
		endpointLimit: endpointLimit,
	}
}

// get is a concurrent safe get operation that will retrieve an endpoint
// based on endpointKey. A boolean will also be returned to illustrate whether
// or not the endpoint had been found.
func (c *EndpointCache) get(endpointKey string) (Endpoint, bool) {
	endpoint, ok := c.endpoints.Load(endpointKey)
	if !ok {
		return Endpoint{}, false
	}

	c.endpoints.Store(endpointKey, endpoint)
	return endpoint.(Endpoint), true
}

// Get will retrieve an endpoint based off of the endpoint key. If an endpoint
// should be retrieved, due to not existing or the current endpoint has expired
// the Discoverer object that was passed in will attempt to discover a new endpoint
// and add that to the cache.
func (c *EndpointCache) Get(d Discoverer, endpointKey string, required bool) (Endpoint, error) {
	endpoint, ok := c.get(endpointKey)
	shouldGet := !ok || endpoint.HasExpired()

	if required && shouldGet {
		if err := c.discover(d, endpointKey); err != nil {
			return Endpoint{}, err
		}
	} else if shouldGet {
		go c.discover(d, endpointKey)
	}

	return endpoint, nil
}

// Add is a concurrent safe operation that will allow new endpoints to be added
// to the cache. If the cache is full, the number of endpoints equal endpointLimit,
// then this will remove the oldest entry before adding the new endpoint.
func (c *EndpointCache) Add(endpoint Endpoint) {
	// de-dups multiple adds of an endpoint with a pre-existing key
	if iface, ok := c.endpoints.Load(endpoint.Key); ok {
		e := iface.(Endpoint)
		if !e.HasExpired() {
			return
		}
	}
	c.endpoints.Store(endpoint.Key, endpoint)

	atomic.AddInt64(&c.size, 1)
	if c.size > 0 && c.size > c.endpointLimit {
		c.deleteRandomKey()
	}
}

// deleteRandomKey will delete a random key from the cache. If
// no key was deleted false will be returned.
func (c *EndpointCache) deleteRandomKey() bool {
	atomic.AddInt64(&c.size, -1)
	found := false

	c.endpoints.Range(func(key, value interface{}) bool {
		found = true
		c.endpoints.Delete(key)

		return false
	})

	return found
}

// discover will get and store and endpoint using the Discoverer.
func (c *EndpointCache) discover(d Discoverer, endpointKey string) error {
	endpoint, err := d.Discover()
	if err != nil {
		return err
	}

	endpoint.Key = endpointKey
	c.Add(endpoint)

	return nil
}
