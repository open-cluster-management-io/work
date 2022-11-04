package auth

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"

	"k8s.io/klog/v2"
)

func NewExecutorCache() *ExecutorCaches {
	return &ExecutorCaches{
		lock:  sync.RWMutex{},
		items: make(map[string]*DimensionCaches),
	}
}

type ExecutorCaches struct {
	lock sync.RWMutex

	// map key: executor in format of {namespace}/{name}
	items map[string]*DimensionCaches
}

type DimensionCaches struct {
	lock sync.RWMutex

	// map key: hash(Dimension)
	items map[string]CacheValue
}

type CacheValue struct {
	Dimension Dimension
	Allowed   bool
}

type Dimension struct {
	Group         string
	Version       string
	Resource      string
	Namespace     string
	Name          string
	ExecuteAction ExecuteAction
}

// AddNewDimensionCaches adds a dimension caches by the executor if it does not exist, updates it if it exists
func (c *ExecutorCaches) AddNewDimensionCaches(executor string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	dimensionCaches := &DimensionCaches{
		lock:  sync.RWMutex{},
		items: make(map[string]CacheValue),
	}
	c.items[executor] = dimensionCaches
}

func (c *ExecutorCaches) GetDimensionCaches(executor string) (*DimensionCaches, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	item, ok := c.items[executor]
	return item, ok
}

func (c *ExecutorCaches) RemoveDimensionCaches(executor string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.items, executor)
}

func (c *ExecutorCaches) GetCacheItems() map[string]*DimensionCaches {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.items
}

// Add adds a cache item if it does not exist, updates the item if it exists
func (c *ExecutorCaches) Add(executor string, dimension Dimension, allowed bool) {
	oldDimensionCaches, ok := c.GetDimensionCaches(executor)
	if !ok {
		c.AddNewDimensionCaches(executor)
		c.Add(executor, dimension, allowed)
		return
	}

	oldDimensionCaches.lock.Lock()
	defer oldDimensionCaches.lock.Unlock()
	oldDimensionCaches.items[dimension.Hash()] = CacheValue{
		Dimension: dimension,
		Allowed:   allowed,
	}
}

// AddWithoutValue adds an empty cache value to the cache
func (c *ExecutorCaches) AddWithEmptyValue(executor string, dimension Dimension) {
	oldDimensionCaches, ok := c.GetDimensionCaches(executor)
	if !ok {
		c.AddNewDimensionCaches(executor)
		c.AddWithEmptyValue(executor, dimension)
		return
	}

	oldDimensionCaches.lock.Lock()
	defer oldDimensionCaches.lock.Unlock()
	oldDimensionCaches.items[dimension.Hash()] = CacheValue{}
}

// GetByHash get an cache item and existence by the dimension
func (c *ExecutorCaches) Get(executor string, dimension Dimension) (bool, bool) {
	oldDimensionCaches, ok := c.GetDimensionCaches(executor)
	if !ok {
		return false, false
	}

	oldDimensionCaches.lock.RLock()
	defer oldDimensionCaches.lock.RUnlock()

	value, ok := oldDimensionCaches.items[dimension.Hash()]
	if !ok {
		return false, false
	}

	return value.Allowed, true
}

// GetByHash get an cache item and existence by the dimension hash
func (c *ExecutorCaches) GetByHash(executor string, hash string) (bool, bool) {
	oldDimensionCaches, ok := c.GetDimensionCaches(executor)
	if !ok {
		return false, false
	}

	oldDimensionCaches.lock.RLock()
	defer oldDimensionCaches.lock.RUnlock()

	value, ok := oldDimensionCaches.items[hash]
	if !ok {
		return false, false
	}

	return value.Allowed, true
}

// RemoveByHash removes an cache item by dimension hash
func (c *ExecutorCaches) RemoveByHash(executor string, hash string) {
	oldDimensionCaches, ok := c.GetDimensionCaches(executor)
	if !ok {
		return
	}

	oldDimensionCaches.lock.RLock()
	defer oldDimensionCaches.lock.RUnlock()

	_, ok = oldDimensionCaches.items[hash]
	if !ok {
		return
	}

	delete(oldDimensionCaches.items, hash)
}

func (c *DimensionCaches) GetCacheItems() map[string]CacheValue {
	c.lock.RLock()
	defer c.lock.RUnlock()
	return c.items
}

func (d *Dimension) Hash() string {
	data, err := json.Marshal(d)
	if err != nil {
		klog.Error("json marshal for dimension %+v error %v", d, err)
		return ""
	}

	h := sha256.New()
	h.Write(data)

	return fmt.Sprintf("%x", h.Sum(nil))
}

func ExecutorKey(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}
