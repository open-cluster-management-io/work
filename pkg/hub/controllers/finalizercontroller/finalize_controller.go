package finalizercontroller

import (
	"context"
	"reflect"
	"time"

	workv1client "github.com/open-cluster-management/api/client/work/clientset/versioned/typed/work/v1"
	worklister "github.com/open-cluster-management/api/client/work/listers/work/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	rbacv1informers "k8s.io/client-go/informers/rbac/v1"
	rbacv1client "k8s.io/client-go/kubernetes/typed/rbac/v1"
	corelisters "k8s.io/client-go/listers/core/v1"
	rbacv1listers "k8s.io/client-go/listers/rbac/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

const (
	manifestWorkFinalizer = "cluster.open-cluster-management.io/manifest-work-cleanup"
)

// RequeueDelay is time to wait before requeue the key
var RequeueDelay = 1 * time.Minute

// FinalizeController ensures all manifestworks are deleted before role/rolebinding for work
// agent are deleted in a terminating cluster namespace.
type FinalizeController struct {
	roleLister         rbacv1listers.RoleLister
	roleBindingLister  rbacv1listers.RoleBindingLister
	rbacClient         rbacv1client.RbacV1Interface
	namespaceLister    corelisters.NamespaceLister
	manifestWorkClient workv1client.WorkV1Interface
	manifestWorkLister worklister.ManifestWorkLister
	eventRecorder      events.Recorder
}

func NewFinalizeController(
	eventRecorder events.Recorder,
	roleInformer rbacv1informers.RoleInformer,
	roleBindingInformer rbacv1informers.RoleBindingInformer,
	namespaceLister corelisters.NamespaceLister,
	manifestWorkClient workv1client.WorkV1Interface,
	manifestWorkLister worklister.ManifestWorkLister,
	rbacClient rbacv1client.RbacV1Interface,
) factory.Controller {

	controller := &FinalizeController{
		roleLister:         roleInformer.Lister(),
		roleBindingLister:  roleBindingInformer.Lister(),
		namespaceLister:    namespaceLister,
		manifestWorkClient: manifestWorkClient,
		manifestWorkLister: manifestWorkLister,
		rbacClient:         rbacClient,
		eventRecorder:      eventRecorder,
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			key, _ := cache.MetaNamespaceKeyFunc(obj)
			return key
		}, roleInformer.Informer(), roleBindingInformer.Informer()).
		WithSync(controller.sync).ToController("FinalizeController", eventRecorder)
}

func (m *FinalizeController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	key := controllerContext.QueueKey()
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		// ignore role/rolebinding whose key is not in format: namespace/name
		return nil
	}

	ns, err := m.namespaceLister.Get(namespace)
	if err != nil {
		return err
	}

	role, rolebinding, err := m.getRoleAndRoleBinding(namespace, name)
	if err != nil {
		return err
	}

	err = m.syncRoleAndRoleBinding(ctx, controllerContext, role, rolebinding, ns)

	if err != nil {
		klog.Errorf("Reconcile role/rolebinding %s fails with err: %v", key, err)
	}
	return err
}

func (m *FinalizeController) syncRoleAndRoleBinding(ctx context.Context, controllerContext factory.SyncContext,
	role *rbacv1.Role, rolebinding *rbacv1.RoleBinding, ns *corev1.Namespace) error {
	key := controllerContext.QueueKey()
	klog.V(4).Infof("Sync role/rolebinding %s", key)

	// no work if neither role nor rolebinding has the finalizer
	if !hasFinalizer(role, manifestWorkFinalizer) && !hasFinalizer(rolebinding, manifestWorkFinalizer) {
		return nil
	}

	if terminated(ns) {
		works, err := m.manifestWorkLister.ManifestWorks(ns.Name).List(labels.Everything())
		if err != nil {
			return err
		}

		// requeue the key if there exists any manifest work in cluster namespace
		if len(works) != 0 {
			controllerContext.Queue().AddAfter(key, RequeueDelay)
			klog.V(4).Infof("Requeue role/rolebinding %q after %v", key, RequeueDelay)
			return nil
		}

		// remove finalizer from role/rolebinding
		if err := m.removeFinalizerFromRole(ctx, role, manifestWorkFinalizer); err != nil {
			return err
		}

		return m.removeFinalizerFromRoleBinding(ctx, rolebinding, manifestWorkFinalizer)
	}

	if terminated(role) {
		if err := m.removeFinalizerFromRole(ctx, role, manifestWorkFinalizer); err != nil {
			return err
		}
	}

	if terminated(rolebinding) {
		if err := m.removeFinalizerFromRoleBinding(ctx, rolebinding, manifestWorkFinalizer); err != nil {
			return err
		}
	}
	return nil
}

func (m *FinalizeController) getRoleAndRoleBinding(namespace, name string) (*rbacv1.Role, *rbacv1.RoleBinding, error) {
	role, err := m.roleLister.Roles(namespace).Get(name)
	if err != nil && !errors.IsNotFound(err) {
		return nil, nil, err
	}

	rolebinding, err := m.roleBindingLister.RoleBindings(namespace).Get(name)
	if err != nil && !errors.IsNotFound(err) {
		return nil, nil, err
	}

	return role, rolebinding, nil
}

// removeFinalizerFromRole removes the particular finalizer from role
func (m *FinalizeController) removeFinalizerFromRole(ctx context.Context, role *rbacv1.Role, finalizer string) error {
	if role == nil {
		return nil
	}

	role = role.DeepCopy()
	if changed := removeFinalizer(role, finalizer); !changed {
		return nil
	}

	_, err := m.rbacClient.Roles(role.Namespace).Update(ctx, role, metav1.UpdateOptions{})
	return err
}

// removeFinalizerFromRoleBinding removes the particular finalizer from rolebinding
func (m *FinalizeController) removeFinalizerFromRoleBinding(ctx context.Context, rolebinding *rbacv1.RoleBinding, finalizer string) error {
	if rolebinding == nil {
		return nil
	}

	rolebinding = rolebinding.DeepCopy()
	if changed := removeFinalizer(rolebinding, finalizer); !changed {
		return nil
	}

	_, err := m.rbacClient.RoleBindings(rolebinding.Namespace).Update(ctx, rolebinding, metav1.UpdateOptions{})
	return err
}

// hasFinalizer returns true if the object has the given finalizer
func hasFinalizer(obj runtime.Object, finalizer string) bool {
	if obj == nil || reflect.ValueOf(obj).IsNil() {
		return false
	}

	accessor, _ := meta.Accessor(obj)
	for _, f := range accessor.GetFinalizers() {
		if f == finalizer {
			return true
		}
	}

	return false
}

// removeFinalizer removes a finalizer from the list. It mutates its input.
func removeFinalizer(obj runtime.Object, finalizerName string) bool {
	if obj == nil || reflect.ValueOf(obj).IsNil() {
		return false
	}

	newFinalizers := []string{}
	accessor, _ := meta.Accessor(obj)
	found := false
	for _, finalizer := range accessor.GetFinalizers() {
		if finalizer == finalizerName {
			found = true
			continue
		}
		newFinalizers = append(newFinalizers, finalizer)
	}
	if found {
		accessor.SetFinalizers(newFinalizers)
	}
	return found
}

// terminated returns true if the DeletionTimestamp of the object is set
func terminated(obj runtime.Object) bool {
	if obj == nil || reflect.ValueOf(obj).IsNil() {
		return false
	}

	accessor, _ := meta.Accessor(obj)
	deletionTimestamp := accessor.GetDeletionTimestamp()

	if deletionTimestamp == nil {
		return false
	}

	if deletionTimestamp.IsZero() {
		return false
	}

	return true
}
