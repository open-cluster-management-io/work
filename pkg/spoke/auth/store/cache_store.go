package store

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sync"

	"k8s.io/klog/v2"
)

// ExecuteAction is the action of executing the manifest work
type ExecuteAction int

const (
	// ApplyAndDeleteAction represents applying(create/update) resource to the managed cluster,
	// and is responsiable for deleting the resource
	ApplyAndDeleteAction ExecuteAction = iota
	// ApplyNoDeleteAction represents only applying(create/update) resource to the managed cluster,
	// but is not responsiable for deleting the resource
	ApplyNoDeleteAction
)

func (a ExecuteAction) String() string {
	return [...]string{"ApplyAndDelete", "ApplyNoDelete"}[a]
}

// GetExecuteAction get the execute action by judging whether a resource is owned by work
func GetExecuteAction(ownedByTheWork bool) ExecuteAction {
	if ownedByTheWork {
		return ApplyAndDeleteAction
	}
	return ApplyNoDeleteAction
}

// GetOwnedByWork judges whether a resource is owned by work according to the execute action
func GetOwnedByWork(action ExecuteAction) bool {
	return action == ApplyAndDeleteAction
}

// NewExecutorCache creates an executor caches
func NewExecutorCache() *ExecutorCaches {
	return &ExecutorCaches{
		lock:  sync.RWMutex{},
		items: make(map[string]*DimensionCaches),
	}
}

// ExecutorCaches is a two-level map cache structure, the 1-level map's key is the executor(service account)
// in format of {namespace}/{name}, and the 2-level map's key is the hash value of the dimension(cached
// subject access review result of a specific resource, group-version-resource-namespace-name-action)
type ExecutorCaches struct {
	lock sync.RWMutex

	// map key: executor in format of {namespace}/{name}
	items map[string]*DimensionCaches
}

// DimensionCaches contains a set of caches for an executor
type DimensionCaches struct {
	lock sync.RWMutex

	// map key: hash(Dimension)
	items map[string]CacheValue
}

// CacheValue contains the cached result and the dimension
type CacheValue struct {
	Dimension Dimension
	Allowed   bool
}

// Dimension represents the dimension of the cache, it determines what the cache is for.
type Dimension struct {
	Group         string
	Version       string
	Resource      string
	Namespace     string
	Name          string
	ExecuteAction ExecuteAction
}

// Add adds a cache item if it does not exist, updates the item if it does
func (c *ExecutorCaches) Add(executor string, dimension Dimension, allowed bool) {
	oldDimensionCaches, ok := c.getDimensionCaches(executor)
	if !ok {
		c.addNewDimensionCaches(executor)
		c.Add(executor, dimension, allowed)
		return
	}

	oldDimensionCaches.add(dimension, allowed)
}

// Get get an cache item and existence by the dimension
func (c *ExecutorCaches) Get(executor string, dimension Dimension) (bool, bool) {
	oldDimensionCaches, ok := c.getDimensionCaches(executor)
	if !ok {
		return false, false
	}

	return oldDimensionCaches.get(dimension.Hash())
}

// RemoveByHash removes an cache item by dimension hash
func (c *ExecutorCaches) RemoveByHash(executor string, hash string) {
	oldDimensionCaches, ok := c.getDimensionCaches(executor)
	if !ok {
		return
	}

	oldDimensionCaches.remove(hash)

	// if the deleted cache is the last element, delete the upper level dimension caches
	if len(oldDimensionCaches.items) == 0 {
		c.removeDimensionCaches(executor)
	}
}

// CleanupUnnecessaryCaches only keeps the necessaryCaches and removes others
func (c *ExecutorCaches) CleanupUnnecessaryCaches(necessaryCaches *ExecutorCaches) {
	for key, caches := range c.getCacheItems() {
		if _, ok := necessaryCaches.getDimensionCaches(key); !ok {
			c.removeDimensionCaches(key)
			klog.V(4).Infof("Remove dimension caches %s", key)
			continue
		}

		for hash := range caches.getCacheItems() {
			if _, ok := necessaryCaches.getByHash(key, hash); !ok {
				c.RemoveByHash(key, hash)
				klog.V(4).Infof("Remove cache item executor %s dimension %s", key, hash)
			}
		}
	}
}

// IterateCacheItems iterates all caches of executorKey and executes fn on it
func (c *ExecutorCaches) IterateCacheItems(executorKey string, fn func(v CacheValue) error) {
	caches, ok := c.getDimensionCaches(executorKey)
	if !ok {
		klog.V(4).Infof("The cache of executor %s has not been initialized", executorKey)
		return
	}

	for hash, item := range caches.getCacheItems() {
		if err := fn(item); err != nil {
			klog.Errorf("Execute function on the cache of executor %s dimension %s failed: %v",
				executorKey, hash, err)
		}
	}
}

// Count counts all cache items
func (c *ExecutorCaches) Count() int {
	count := 0
	for _, caches := range c.getCacheItems() {
		for range caches.getCacheItems() {
			count++
		}
	}
	return count
}

// DimensionCachesExists returns if the dimension caches of the executor exists
func (c *ExecutorCaches) DimensionCachesExists(executor string) bool {
	c.lock.RLock()
	defer c.lock.RUnlock()
	_, ok := c.items[executor]
	return ok
}

// addNewDimensionCaches adds a dimension caches by the executor if it does not exist, updates it if it does
func (c *ExecutorCaches) addNewDimensionCaches(executor string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	dimensionCaches := &DimensionCaches{
		lock:  sync.RWMutex{},
		items: make(map[string]CacheValue),
	}
	c.items[executor] = dimensionCaches
}

func (c *ExecutorCaches) getDimensionCaches(executor string) (*DimensionCaches, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	item, ok := c.items[executor]
	return item, ok
}

func (c *ExecutorCaches) removeDimensionCaches(executor string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.items, executor)
}

func (c *ExecutorCaches) getCacheItems() map[string]*DimensionCaches {
	c.lock.Lock()
	defer c.lock.Unlock()
	return c.items
}

// getByHash get an cache item and existence by the dimension hash
func (c *ExecutorCaches) getByHash(executor string, hash string) (bool, bool) {
	oldDimensionCaches, ok := c.getDimensionCaches(executor)
	if !ok {
		return false, false
	}

	return oldDimensionCaches.get(hash)
}

func (c *DimensionCaches) remove(hash string) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	_, ok := c.items[hash]
	if !ok {
		return
	}

	delete(c.items, hash)
}

func (c *DimensionCaches) get(hash string) (bool, bool) {
	c.lock.RLock()
	defer c.lock.RUnlock()

	value, ok := c.items[hash]
	if !ok {
		return false, false
	}
	return value.Allowed, true
}

func (c *DimensionCaches) add(dimension Dimension, allowed bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.items[dimension.Hash()] = CacheValue{
		Dimension: dimension,
		Allowed:   allowed,
	}
}

func (c *DimensionCaches) getCacheItems() map[string]CacheValue {
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

// ExecutorKey return a key of executor caches map
func ExecutorKey(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}
