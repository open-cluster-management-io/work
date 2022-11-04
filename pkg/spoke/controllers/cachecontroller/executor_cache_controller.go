package cachecontroller

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
	"open-cluster-management.io/work/pkg/spoke/auth"
)

var (
	ResyncInterval = 10 * time.Minute
)

// CacheController is to reconcile the executor auth result for manfiestwork workloads
// on spoke cluster.
type CacheController struct {
	manifestWorkLister worklister.ManifestWorkNamespaceLister
	executorCaches     *auth.ExecutorCaches
	sarCheckerFn       auth.SubjectAccessReviewCheckFn
	restMapper         meta.RESTMapper
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
	executorCaches *auth.ExecutorCaches,
	sarCheckerFn auth.SubjectAccessReviewCheckFn,
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

	controller := &CacheController{
		manifestWorkLister: manifestWorkLister,
		restMapper:         restMapper,
		executorCaches:     executorCaches,
		sarCheckerFn:       sarCheckerFn,
	}

	return factory.New().
		WithInformersQueueKeysFunc(
			roleEnqueueFu(rbInformer.Informer().GetIndexer(), executorCaches),
			rInformer.Informer()).
		WithInformersQueueKeysFunc(
			clusterRoleEnqueueFu(rbInformer.Informer().GetIndexer(), crbInformer.Informer().GetIndexer(), executorCaches),
			crInformer.Informer()).
		WithInformersQueueKeysFunc(
			roleBindingEnqueueFu(executorCaches),
			rbInformer.Informer()).
		WithInformersQueueKeysFunc(
			clusterRoleBindingEnqueueFu(executorCaches),
			rbInformer.Informer()).
		WithSync(controller.sync).
		ResyncEvery(ResyncInterval). // cleanup unnecessary cache every ResyncInterval
		ToController("ManifestWorkExecutorCache", recorder)
}

func roleEnqueueFu(rbIndexer cache.Indexer, executorCaches *auth.ExecutorCaches) func(runtime.Object) []string {
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
	executorCaches *auth.ExecutorCaches) func(runtime.Object) []string {
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

func roleBindingEnqueueFu(executorCaches *auth.ExecutorCaches) func(runtime.Object) []string {
	return func(obj runtime.Object) []string {
		ret := make([]string, 0)

		if rb, ok := obj.(*rbacapiv1.RoleBinding); ok {
			executors := getInterestedExecutors(rb.Subjects, executorCaches)
			ret = append(ret, executors...)
		}

		return ret
	}
}

func clusterRoleBindingEnqueueFu(executorCaches *auth.ExecutorCaches) func(runtime.Object) []string {
	return func(obj runtime.Object) []string {
		ret := make([]string, 0)

		if crb, ok := obj.(*rbacapiv1.ClusterRoleBinding); ok {
			executors := getInterestedExecutors(crb.Subjects, executorCaches)
			ret = append(ret, executors...)
		}

		return ret
	}
}

func getInterestedExecutors(subjects []rbacapiv1.Subject, executorCaches *auth.ExecutorCaches) []string {
	executors := make([]string, 0)
	for _, subject := range subjects {
		if subject.Kind == "ServiceAccount" {
			executor := auth.ExecutorKey(subject.Namespace, subject.Name)
			if _, ok := executorCaches.GetDimensionCaches(executor); ok {
				executors = append(executors, executor)
			}
		}
	}
	return executors
}

// sync is the main reconcile loop for executors. It is triggered when RBAC resources(
// role, rolebinding, clusterrole, clusterrolebinding) for the executor changed
func (m *CacheController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	executorKey := controllerContext.QueueKey()
	klog.V(4).Infof("Executor cache sync, executorKey: %v", executorKey)
	if executorKey == "key" {
		// cleanup unnecessary cache
		klog.V(4).Infof("There are %v cache items before cleanup", m.countCache())
		m.cleanupUnnecessaryCache()
		klog.V(4).Infof("There are %v cache items after cleanup", m.countCache())
		return nil
	}

	saNamespace, saName, err := cache.SplitMetaNamespaceKey(executorKey)
	if err != nil {
		// ignore executor whose key is not in format: namespace/name
		return nil
	}

	caches, ok := m.executorCaches.GetDimensionCaches(executorKey)
	if !ok {
		klog.V(4).Infof("The cache of executor %s has not been initialized", executorKey)
		return nil
	}

	for _, item := range caches.GetCacheItems() {
		err = m.sarCheckerFn(ctx, &workapiv1.ManifestWorkSubjectServiceAccount{
			Namespace: saNamespace,
			Name:      saName,
		}, schema.GroupVersionResource{
			Group:    item.Dimension.Group,
			Version:  item.Dimension.Version,
			Resource: item.Dimension.Resource,
		},
			item.Dimension.Namespace, item.Dimension.Name, auth.OwnedByWork(item.Dimension.ExecuteAction))

		klog.V(4).Infof("Update executor cache for executorKey: %s, dimension: %+v result: %v",
			executorKey, item.Dimension, err)
		auth.UpdateSARCheckResultToCache(m.executorCaches, executorKey, item.Dimension, err)
	}
	return nil
}

func (m *CacheController) cleanupUnnecessaryCache() {
	retainableCache := m.getAllValuableCache()
	for key, caches := range m.executorCaches.GetCacheItems() {
		if _, ok := retainableCache.GetDimensionCaches(key); !ok {
			m.executorCaches.RemoveDimensionCaches(key)
			klog.V(4).Infof("Remove dimension caches %s", key)
			continue
		}

		for hash := range caches.GetCacheItems() {
			if _, ok := retainableCache.GetByHash(key, hash); !ok {
				m.executorCaches.RemoveByHash(key, hash)
				klog.V(4).Infof("Remove cache item executor %s dimension %s", key, hash)
			}
		}
	}
}

func (m *CacheController) getAllValuableCache() *auth.ExecutorCaches {
	retainableCache := auth.NewExecutorCache()

	mws, err := m.manifestWorkLister.List(labels.Everything())
	if err != nil {
		klog.Infof("cleanup cache, list manifestworks error: %v", err)
		return retainableCache
	}

	for _, mw := range mws {
		var executorKey string
		if mw.Spec.Executor == nil {
			continue
		}
		if mw.Spec.Executor.Subject.Type != workapiv1.ExecutorSubjectTypeServiceAccount {
			continue
		}
		if mw.Spec.Executor.Subject.ServiceAccount == nil {
			continue
		}

		executorKey = auth.ExecutorKey(
			mw.Spec.Executor.Subject.ServiceAccount.Namespace, mw.Spec.Executor.Subject.ServiceAccount.Name)

		for _, manifest := range mw.Status.ResourceStatus.Manifests {

			mapping, err := m.restMapper.RESTMapping(schema.GroupKind{
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

			retainableCache.AddWithEmptyValue(executorKey, auth.Dimension{
				Group:         manifest.ResourceMeta.Group,
				Version:       manifest.ResourceMeta.Version,
				Resource:      gvr.Resource,
				Namespace:     manifest.ResourceMeta.Namespace,
				Name:          manifest.ResourceMeta.Name,
				ExecuteAction: auth.GetExecuteAction(ownedByTheWork),
			})
		}
	}

	return retainableCache
}

func (m *CacheController) countCache() int {
	count := 0
	for _, caches := range m.executorCaches.GetCacheItems() {
		for range caches.GetCacheItems() {
			count++
		}
	}
	return count
}
