package cache

import (
	rbacapiv1 "k8s.io/api/rbac/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// type BindingTypes interface {
// 	*rbacapiv1.RoleBinding | *rbacapiv1.ClusterRoleBinding
// }

type roleBindingEventHandler struct {
	enqueueUpsertFunc func(key string, subjects []rbacapiv1.Subject)
	enqueueDeleteFunc func(key string, subjects []rbacapiv1.Subject)
}

func (h *roleBindingEventHandler) OnAdd(obj interface{}) {
	rb, ok := obj.(*rbacapiv1.RoleBinding)
	if ok {
		key, _ := cache.MetaNamespaceKeyFunc(rb)
		h.enqueueUpsertFunc(key, rb.Subjects)
	}
}

func (h *roleBindingEventHandler) OnUpdate(oldObj, newObj interface{}) {
	h.OnAdd(newObj)
}

func (h *roleBindingEventHandler) OnDelete(obj interface{}) {

	if rb, ok := obj.(*rbacapiv1.RoleBinding); ok {
		key, _ := cache.MetaNamespaceKeyFunc(rb)
		h.enqueueDeleteFunc(key, rb.Subjects)
		return
	}

	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("Get key for deletion event error %v", err)
		return
	}
	h.enqueueDeleteFunc(key, nil)
}

type clusterRoleBindingEventHandler struct {
	enqueueUpsertFunc func(key string, subjects []rbacapiv1.Subject)
	enqueueDeleteFunc func(key string, subjects []rbacapiv1.Subject)
}

func (h *clusterRoleBindingEventHandler) OnAdd(obj interface{}) {
	crb, ok := obj.(*rbacapiv1.ClusterRoleBinding)
	if ok {
		key, _ := cache.MetaNamespaceKeyFunc(crb)
		h.enqueueUpsertFunc(key, crb.Subjects)
	}
}

func (h *clusterRoleBindingEventHandler) OnUpdate(oldObj, newObj interface{}) {
	h.OnAdd(newObj)
}

func (h *clusterRoleBindingEventHandler) OnDelete(obj interface{}) {

	if crb, ok := obj.(*rbacapiv1.ClusterRoleBinding); ok {
		key, _ := cache.MetaNamespaceKeyFunc(crb)
		h.enqueueDeleteFunc(key, crb.Subjects)
		return
	}

	key, err := cache.DeletionHandlingMetaNamespaceKeyFunc(obj)
	if err != nil {
		klog.Errorf("Get key for deletion event error %v", err)
		return
	}
	h.enqueueDeleteFunc(key, nil)
}
