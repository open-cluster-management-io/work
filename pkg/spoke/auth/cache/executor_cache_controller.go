package cache

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	rbacapiv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	rbacv1 "k8s.io/client-go/informers/rbac/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/work/pkg/spoke/auth/store"
)

var (
	ResyncInterval = 10 * time.Minute
)

// CacheController is to reconcile the executor auth result for manfiestwork workloads
// on spoke cluster.
type CacheController struct {
	executorCaches                   *store.ExecutorCaches
	sarCheckerFn                     SubjectAccessReviewCheckFn
	manifestWorkExecutorCachesLoader manifestWorkExecutorCachesLoader
	bindingExecutorsMapper           *safeMap
}

// NewExecutorCacheController returns an ExecutorCacheController, the controller will watch all the RBAC resources(role,
// rolebinding, clusterrole, clusterrolebinding) related to the executors used by the manifestworks, and update the
// caches of the corresponding executor when the RBAC resources change
func NewExecutorCacheController(
	ctx context.Context,
	recorder events.Recorder,
	crbInformer rbacv1.ClusterRoleBindingInformer,
	rbInformer rbacv1.RoleBindingInformer,
	crInformer rbacv1.ClusterRoleInformer,
	rInformer rbacv1.RoleInformer,
	manifestWorkExecutorCachesLoader manifestWorkExecutorCachesLoader,
	executorCaches *store.ExecutorCaches,
	sarCheckerFn SubjectAccessReviewCheckFn,
) factory.Controller {

	controller := &CacheController{
		manifestWorkExecutorCachesLoader: manifestWorkExecutorCachesLoader,
		executorCaches:                   executorCaches,
		sarCheckerFn:                     sarCheckerFn,
		bindingExecutorsMapper:           newSafeMap(),
	}

	return newControllerInner(controller, recorder, crbInformer, rbInformer, crInformer, rInformer)
}

func newSafeMap() *safeMap {
	return &safeMap{
		lock:  sync.RWMutex{},
		items: make(map[string][]string),
	}
}

// newControllerInner is an inner function to create a cache controller,
// this is useful for unit test to fake the value of CacheController
func newControllerInner(controller *CacheController,
	recorder events.Recorder,
	crbInformer rbacv1.ClusterRoleBindingInformer,
	rbInformer rbacv1.RoleBindingInformer,
	crInformer rbacv1.ClusterRoleInformer,
	rInformer rbacv1.RoleInformer,
) factory.Controller {
	err := crbInformer.Informer().AddIndexers(
		cache.Indexers{
			byClusterRole: crbIndexByClusterRole,
		},
	)
	if err != nil {
		utilruntime.HandleError(err)
	}

	err = rbInformer.Informer().AddIndexers(
		cache.Indexers{
			byClusterRole: rbIndexByClusterRole,
			byRole:        rbIndexByRole,
		},
	)
	if err != nil {
		utilruntime.HandleError(err)
	}

	cacheControllerName := "ManifestWorkExecutorCache"
	syncCtx := factory.NewSyncContext(cacheControllerName, recorder)

	rbInformer.Informer().AddEventHandler(&roleBindingEventHandler{
		enqueueUpsertFunc: controller.bindingResourceUpsertEnqueueFn(syncCtx),
		enqueueDeleteFunc: controller.bindingResourceDeleteEnqueueFn(syncCtx),
	})

	crbInformer.Informer().AddEventHandler(&clusterRoleBindingEventHandler{
		enqueueUpsertFunc: controller.bindingResourceUpsertEnqueueFn(syncCtx),
		enqueueDeleteFunc: controller.bindingResourceDeleteEnqueueFn(syncCtx),
	})

	return factory.New().
		WithSyncContext(syncCtx).
		WithInformersQueueKeysFunc(
			roleEnqueueFu(rbInformer.Informer().GetIndexer(), controller.executorCaches),
			rInformer.Informer()).
		WithInformersQueueKeysFunc(
			clusterRoleEnqueueFu(
				rbInformer.Informer().GetIndexer(), crbInformer.Informer().GetIndexer(),
				controller.executorCaches),
			crInformer.Informer()).
		WithBareInformers(rbInformer.Informer(), crbInformer.Informer()).
		WithSync(controller.sync).
		ResyncEvery(ResyncInterval). // cleanup unnecessary cache every ResyncInterval
		ToController(cacheControllerName, recorder)
}

func roleEnqueueFu(rbIndexer cache.Indexer, executorCaches *store.ExecutorCaches) func(runtime.Object) []string {
	return func(obj runtime.Object) []string {
		accessor, _ := meta.Accessor(obj)
		ret := make([]string, 0)

		roleKey := fmt.Sprintf("%s/%s", accessor.GetNamespace(), accessor.GetName())
		items, err := rbIndexer.ByIndex(byRole, roleKey)
		if err != nil {
			klog.V(4).Infof("RoleBinding indexer get RoleBinding by %s index %s error: %v", byRole, roleKey, err)
		} else {
			for _, item := range items {
				if rb, ok := item.(*rbacapiv1.RoleBinding); ok {
					executors := getInterestedExecutors(rb.Subjects, executorCaches)
					ret = append(ret, executors...)
				}
			}
		}

		return ret
	}
}

func clusterRoleEnqueueFu(rbIndexer cache.Indexer, crbIndexer cache.Indexer,
	executorCaches *store.ExecutorCaches) func(runtime.Object) []string {
	return func(obj runtime.Object) []string {
		accessor, _ := meta.Accessor(obj)
		ret := make([]string, 0)

		clusterRoleKey := accessor.GetName()
		items, err := rbIndexer.ByIndex(byRole, clusterRoleKey)
		if err != nil {
			klog.V(4).Infof("RoleBinding indexer get RoleBinding by %s index %s error: %v",
				byRole, clusterRoleKey, err)
		} else {
			for _, item := range items {
				if rb, ok := item.(*rbacapiv1.RoleBinding); ok {
					executors := getInterestedExecutors(rb.Subjects, executorCaches)
					ret = append(ret, executors...)
				}
			}
		}

		items, err = crbIndexer.ByIndex(byClusterRole, clusterRoleKey)
		if err != nil {
			klog.V(4).Infof("ClusterRoleBinding indexer get ClusterRoleBinding by %s index %s error: %v",
				byClusterRole, clusterRoleKey, err)
		} else {
			for _, item := range items {
				if crb, ok := item.(*rbacapiv1.ClusterRoleBinding); ok {
					executors := getInterestedExecutors(crb.Subjects, executorCaches)
					ret = append(ret, executors...)
				}
			}
		}

		return ret
	}
}

func (c *CacheController) bindingResourceUpsertEnqueueFn(
	syncCtx factory.SyncContext) func(key string, subjects []rbacapiv1.Subject) {

	return func(key string, subjects []rbacapiv1.Subject) {
		executors := getInterestedExecutors(subjects, c.executorCaches)
		for _, executor := range executors {
			syncCtx.Queue().Add(executor)
		}
		if len(executors) > 0 {
			c.bindingExecutorsMapper.upsert(key, executors)
			klog.V(4).Infof("Binding executor mapper upsert key %s executors %s", key, executors)
		}
	}
}

func (c *CacheController) bindingResourceDeleteEnqueueFn(
	syncCtx factory.SyncContext) func(key string, subjects []rbacapiv1.Subject) {

	return func(key string, subjects []rbacapiv1.Subject) {
		enqueued := false
		if subjects != nil {
			for _, executor := range getInterestedExecutors(subjects, c.executorCaches) {
				syncCtx.Queue().Add(executor)
				enqueued = true
			}
		} else {
			for _, executor := range c.bindingExecutorsMapper.get(key) {
				syncCtx.Queue().Add(executor)
				enqueued = true
				klog.V(4).Infof("Deletion event, enqueue executor %s from binding executor mapper key %s", executor, key)
			}
		}

		if enqueued {
			c.bindingExecutorsMapper.delete(key)
			klog.V(4).Infof("Binding executor mapper delete key %s", key)
		}
	}
}

func getInterestedExecutors(subjects []rbacapiv1.Subject, executorCaches *store.ExecutorCaches) []string {
	executors := make([]string, 0)
	for _, subject := range subjects {
		if subject.Kind == "ServiceAccount" {
			executor := store.ExecutorKey(subject.Namespace, subject.Name)
			if ok := executorCaches.DimensionCachesExists(executor); ok {
				executors = append(executors, executor)
			}
		}
	}
	return executors
}

// sync is the main reconcile loop for executors. It is triggered when RBAC resources(
// role, rolebinding, clusterrole, clusterrolebinding) for the executor changed
func (c *CacheController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	executorKey := controllerContext.QueueKey()
	klog.V(4).Infof("Executor cache sync, executorKey: %v", executorKey)
	if executorKey == "key" {
		// cleanup unnecessary cache
		klog.V(4).Infof("There are %v cache items before cleanup", c.executorCaches.Count())
		c.cleanupUnnecessaryCache()
		klog.V(4).Infof("There are %v cache items after cleanup", c.executorCaches.Count())
		return nil
	}

	saNamespace, saName, err := cache.SplitMetaNamespaceKey(executorKey)
	if err != nil {
		// ignore executor whose key is not in format: namespace/name
		return nil
	}

	c.executorCaches.IterateCacheItems(executorKey, c.iterateCacheItemsFn(ctx, executorKey, saNamespace, saName))
	return nil
}

func (c *CacheController) iterateCacheItemsFn(ctx context.Context,
	executorKey, saNamespace, saName string) func(v store.CacheValue) error {
	return func(v store.CacheValue) error {
		err := c.sarCheckerFn(ctx, &workapiv1.ManifestWorkSubjectServiceAccount{
			Namespace: saNamespace,
			Name:      saName,
		}, schema.GroupVersionResource{
			Group:    v.Dimension.Group,
			Version:  v.Dimension.Version,
			Resource: v.Dimension.Resource,
		},
			v.Dimension.Namespace, v.Dimension.Name, store.GetOwnedByWork(v.Dimension.ExecuteAction))

		klog.V(4).Infof("Update executor cache for executorKey: %s, dimension: %+v result: %v",
			executorKey, v.Dimension, err)
		updateSARCheckResultToCache(c.executorCaches, executorKey, v.Dimension, err)
		return nil
	}
}

func (c *CacheController) cleanupUnnecessaryCache() {
	retainableCache := store.NewExecutorCache()
	c.manifestWorkExecutorCachesLoader.loadAllValuableCaches(retainableCache)
	c.executorCaches.CleanupUnnecessaryCaches(retainableCache)
}

type safeMap struct {
	lock  sync.RWMutex
	items map[string][]string
}

func (m *safeMap) upsert(k string, v []string) {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.items[k] = v
}

func (m *safeMap) delete(k string) {
	m.lock.Lock()
	defer m.lock.Unlock()
	delete(m.items, k)
}

func (m *safeMap) get(k string) []string {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return m.items[k]
}

func (m *safeMap) count() int {
	m.lock.RLock()
	defer m.lock.RUnlock()
	return len(m.items)
}
