package hubtesting

import (
	"time"

	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/workqueue"
)

func NewRole(namespace, name string, finalizers []string, terminated bool) *rbacv1.Role {
	role := &rbacv1.Role{}

	role.Namespace = namespace
	role.Name = name
	role.Finalizers = finalizers
	if terminated {
		now := metav1.Now()
		role.DeletionTimestamp = &now
	}

	return role
}

func NewRoleBinding(namespace, name string, finalizers []string, terminated bool) *rbacv1.RoleBinding {
	rolebinding := &rbacv1.RoleBinding{}

	rolebinding.Namespace = namespace
	rolebinding.Name = name
	rolebinding.Finalizers = finalizers
	if terminated {
		now := metav1.Now()
		rolebinding.DeletionTimestamp = &now
	}

	return rolebinding
}

func NewNamespace(name string, terminated bool) *corev1.Namespace {
	namespace := &corev1.Namespace{}
	namespace.Name = name
	if terminated {
		now := metav1.Now()
		namespace.DeletionTimestamp = &now
	}

	return namespace
}

func NewManifestWork(namespace, name string, finalizers []string, deletionTimestamp *metav1.Time) *workapiv1.ManifestWork {
	work := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:         namespace,
			Name:              name,
			Finalizers:        finalizers,
			DeletionTimestamp: deletionTimestamp,
		},
	}

	return work
}

func NewDeletionTimestamp(offset time.Duration) *metav1.Time {
	return &metav1.Time{
		Time: metav1.Now().Add(offset),
	}
}

type syncContext struct {
	eventRecorder events.Recorder
	queueKey      string
	queue         workqueue.RateLimitingInterface
}

func (c syncContext) Queue() workqueue.RateLimitingInterface {
	return c.queue
}

func (c syncContext) QueueKey() string {
	return c.queueKey
}

func (c syncContext) Recorder() events.Recorder {
	return c.eventRecorder
}

func NewSyncContext(queueKey string, eventRecorder events.Recorder) factory.SyncContext {
	return &syncContext{
		queueKey:      queueKey,
		eventRecorder: eventRecorder,
		queue:         workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
	}
}
