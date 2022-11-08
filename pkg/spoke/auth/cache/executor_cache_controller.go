package cache

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	rbacapiv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	rbacv1 "k8s.io/client-go/informers/rbac/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/helper"
	"open-cluster-management.io/work/pkg/spoke/auth/store"
)

var (
	ResyncInterval = 10 * time.Minute
)

// CacheController is to reconcile the executor auth result for manfiestwork workloads
// on spoke cluster.
type CacheController struct {
	manifestWorkLister worklister.ManifestWorkNamespaceLister
	executorCaches     *store.ExecutorCaches
	sarCheckerFn       SubjectAccessReviewCheckFn
	restMapper         meta.RESTMapper

	roleBindingExecutorsMapper        map[string][]string
	clusterRoleBindingExecutorsMapper map[string][]string
}

// NewExecutorCacheController returns an ExecutorCacheController
func NewExecutorCacheController(
	ctx context.Context,
	recorder events.Recorder,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	crbInformer rbacv1.ClusterRoleBindingInformer,
	rbInformer rbacv1.RoleBindingInformer,
	crInformer rbacv1.ClusterRoleInformer,
	rInformer rbacv1.RoleInformer,
	restMapper meta.RESTMapper,
	executorCaches *store.ExecutorCaches,
	sarCheckerFn SubjectAccessReviewCheckFn,
) factory.Controller {

	controller := &CacheController{
		manifestWorkLister:                manifestWorkLister,
		restMapper:                        restMapper,
		executorCaches:                    executorCaches,
		sarCheckerFn:                      sarCheckerFn,
		roleBindingExecutorsMapper:        make(map[string][]string),
		clusterRoleBindingExecutorsMapper: make(map[string][]string),
	}

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
		enqueueUpsertFunc: controller.bindingResourceUpsertEnqueueFn(syncCtx, controller.roleBindingExecutorsMapper),
		enqueueDeleteFunc: controller.bindingResourceDeleteEnqueueFn(syncCtx, controller.roleBindingExecutorsMapper),
	})

	crbInformer.Informer().AddEventHandler(&clusterRoleBindingEventHandler{
		enqueueUpsertFunc: controller.bindingResourceUpsertEnqueueFn(
			syncCtx, controller.clusterRoleBindingExecutorsMapper),
		enqueueDeleteFunc: controller.bindingResourceDeleteEnqueueFn(
			syncCtx, controller.clusterRoleBindingExecutorsMapper),
	})

	return factory.New().
		WithSyncContext(syncCtx).
		WithInformersQueueKeysFunc(
			roleEnqueueFu(rbInformer.Informer().GetIndexer(), executorCaches),
			rInformer.Informer()).
		WithInformersQueueKeysFunc(
			clusterRoleEnqueueFu(rbInformer.Informer().GetIndexer(), crbInformer.Informer().GetIndexer(), executorCaches),
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
	syncCtx factory.SyncContext,
	bindingExecutorsMapper map[string][]string,
) func(key string, subjects []rbacapiv1.Subject) {

	return func(key string, subjects []rbacapiv1.Subject) {
		executors := getInterestedExecutors(subjects, c.executorCaches)
		for _, executor := range executors {
			syncCtx.Queue().Add(executor)
		}
		if len(executors) > 0 {
			bindingExecutorsMapper[key] = executors
			klog.V(4).Infof("binding executor mapper upsert key %s executors %s", key, executors)
		}
	}
}

func (c *CacheController) bindingResourceDeleteEnqueueFn(
	syncCtx factory.SyncContext,
	bindingExecutorsMapper map[string][]string,
) func(key string, subjects []rbacapiv1.Subject) {

	return func(key string, subjects []rbacapiv1.Subject) {
		enqueued := false
		if subjects != nil {
			for _, executor := range getInterestedExecutors(subjects, c.executorCaches) {
				syncCtx.Queue().Add(executor)
				enqueued = true
			}
		} else {
			for _, executor := range bindingExecutorsMapper[key] {
				syncCtx.Queue().Add(executor)
				enqueued = true
				klog.V(4).Infof("deletion event, enqueue executor %s from binding executor mapper key %s", executor, key)
			}
		}

		if enqueued {
			delete(bindingExecutorsMapper, key)
			klog.V(4).Infof("binding executor mapper delete key %s", key)
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
		return err
	}
}

func (c *CacheController) cleanupUnnecessaryCache() {
	c.executorCaches.CleanupUnnecessaryCaches(c.getAllValuableCache())
}

func (c *CacheController) getAllValuableCache() *store.ExecutorCaches {
	retainableCache := store.NewExecutorCache()

	mws, err := c.manifestWorkLister.List(labels.Everything())
	if err != nil {
		klog.Infof("cleanup cache, list manifestworks error: %v", err)
		return retainableCache
	}

	for _, mw := range mws {
		var executor string
		if mw.Spec.Executor == nil {
			continue
		}
		if mw.Spec.Executor.Subject.Type != workapiv1.ExecutorSubjectTypeServiceAccount {
			continue
		}
		if mw.Spec.Executor.Subject.ServiceAccount == nil {
			continue
		}

		executor = store.ExecutorKey(
			mw.Spec.Executor.Subject.ServiceAccount.Namespace, mw.Spec.Executor.Subject.ServiceAccount.Name)

		for _, manifest := range mw.Status.ResourceStatus.Manifests {

			mapping, err := c.restMapper.RESTMapping(schema.GroupKind{
				Group: manifest.ResourceMeta.Group,
				Kind:  manifest.ResourceMeta.Kind,
			}, manifest.ResourceMeta.Version)
			if err != nil {
				klog.Infof("the server doesn't have a resource type %q", manifest.ResourceMeta.Kind)
				continue
			}

			gvr := mapping.Resource
			// check if the resource to be applied should be owned by the manifest work
			ownedByTheWork := helper.OwnedByTheWork(gvr, manifest.ResourceMeta.Namespace, manifest.ResourceMeta.Name,
				mw.Spec.DeleteOption)

			retainableCache.Add(executor, store.Dimension{
				Group:         manifest.ResourceMeta.Group,
				Version:       manifest.ResourceMeta.Version,
				Resource:      gvr.Resource,
				Namespace:     manifest.ResourceMeta.Namespace,
				Name:          manifest.ResourceMeta.Name,
				ExecuteAction: store.GetExecuteAction(ownedByTheWork),
			},
				false, // use false as a fake value
			)
		}
	}

	return retainableCache
}
