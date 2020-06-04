package finalizercontroller

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	fakeworkclient "github.com/open-cluster-management/api/client/work/clientset/versioned/fake"
	workinformers "github.com/open-cluster-management/api/client/work/informers/externalversions"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/open-cluster-management/work/pkg/hub/hubtesting"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	fakeclient "k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/utils/diff"
)

func TestSyncRoleAndRoleBinding(t *testing.T) {
	cases := []struct {
		name                          string
		role                          *rbacv1.Role
		roleBinding                   *rbacv1.RoleBinding
		namespace                     *corev1.Namespace
		work                          *workapiv1.ManifestWork
		expectedRoleFinalizers        []string
		expectedRoleBindingFinalizers []string
		expectedWorkFinalizers        []string
		expectedQueueLen              int
		validateRabcActions           func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:                "skip if neither role nor rolebinding exists",
			namespace:           hubtesting.NewNamespace("cluster1", false),
			work:                hubtesting.NewManifestWork("cluster1", "work1", nil, nil),
			validateRabcActions: noAction,
		},
		{
			name:                   "skip if neither role nor rolebinding has finalizer",
			role:                   hubtesting.NewRole("cluster1", "cluster1:spoke-work", nil, false),
			roleBinding:            hubtesting.NewRoleBinding("cluster1", "cluster1:spoke-work", nil, false),
			namespace:              hubtesting.NewNamespace("cluster1", false),
			work:                   hubtesting.NewManifestWork("cluster1", "work1", []string{manifestWorkFinalizer}, nil),
			expectedWorkFinalizers: []string{manifestWorkFinalizer},
			validateRabcActions:    noAction,
		},
		{
			name:                          "remove finalizer from deleting role within non-terminating namespace",
			role:                          hubtesting.NewRole("cluster1", "cluster1:spoke-work", []string{manifestWorkFinalizer}, true),
			roleBinding:                   hubtesting.NewRoleBinding("cluster1", "cluster1:spoke-work", []string{manifestWorkFinalizer}, false),
			namespace:                     hubtesting.NewNamespace("cluster1", false),
			work:                          hubtesting.NewManifestWork("cluster1", "work1", []string{manifestWorkFinalizer}, nil),
			expectedRoleBindingFinalizers: []string{manifestWorkFinalizer},
			expectedWorkFinalizers:        []string{manifestWorkFinalizer},
			validateRabcActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name:        "remove finalizer from role/rolebinding within terminating namespace",
			role:        hubtesting.NewRole("cluster1", "cluster1:spoke-work", []string{manifestWorkFinalizer}, true),
			roleBinding: hubtesting.NewRoleBinding("cluster1", "cluster1:spoke-work", []string{manifestWorkFinalizer}, true),
			namespace:   hubtesting.NewNamespace("cluster1", true),
			validateRabcActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
		{
			name:                          "requeue the key",
			role:                          hubtesting.NewRole("cluster1", "cluster1:spoke-work", []string{manifestWorkFinalizer}, true),
			roleBinding:                   hubtesting.NewRoleBinding("cluster1", "cluster1:spoke-work", []string{manifestWorkFinalizer}, true),
			namespace:                     hubtesting.NewNamespace("cluster1", true),
			work:                          hubtesting.NewManifestWork("cluster1", "work1", []string{manifestWorkFinalizer}, hubtesting.NewDeletionTimestamp(0)),
			expectedRoleFinalizers:        []string{manifestWorkFinalizer},
			expectedRoleBindingFinalizers: []string{manifestWorkFinalizer},
			expectedWorkFinalizers:        []string{manifestWorkFinalizer},
			expectedQueueLen:              1,
			validateRabcActions:           noAction,
		},
	}

	// reduce the delay to speed up the testing
	RequeueDelay = 0

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			if c.role != nil {
				objects = append(objects, c.role)
			}
			if c.roleBinding != nil {
				objects = append(objects, c.roleBinding)
			}
			if c.namespace != nil {
				objects = append(objects, c.namespace)
			}
			fakeClient := fakeclient.NewSimpleClientset(objects...)

			var fakeManifestWorkClient *fakeworkclient.Clientset
			if c.work == nil {
				fakeManifestWorkClient = fakeworkclient.NewSimpleClientset()
			} else {
				fakeManifestWorkClient = fakeworkclient.NewSimpleClientset(c.work)
			}
			workInformerFactory := workinformers.NewSharedInformerFactory(fakeManifestWorkClient, 5*time.Minute)

			recorder := events.NewInMemoryRecorder("")
			controller := FinalizeController{
				manifestWorkClient: fakeManifestWorkClient.WorkV1(),
				manifestWorkLister: workInformerFactory.Work().V1().ManifestWorks().Lister(),
				eventRecorder:      recorder,
				rbacClient:         fakeClient.RbacV1(),
			}

			controllerContext := hubtesting.NewSyncContext("", recorder)

			func() {
				ctx, cancel := context.WithTimeout(context.TODO(), 15*time.Second)
				defer cancel()

				workInformerFactory.Start(ctx.Done())
				workInformerFactory.WaitForCacheSync(ctx.Done())

				controller.syncRoleAndRoleBinding(context.TODO(), controllerContext, c.role, c.roleBinding, c.namespace)

				c.validateRabcActions(t, fakeClient.Actions())

				if c.role != nil {
					role, err := fakeClient.RbacV1().Roles(c.role.Namespace).Get(context.TODO(), c.role.Name, metav1.GetOptions{})
					if err != nil {
						t.Fatal(err)
					}
					assertFinalizers(t, role, c.expectedRoleFinalizers)
				}

				if c.roleBinding != nil {
					rolebinding, err := fakeClient.RbacV1().RoleBindings(c.roleBinding.Namespace).Get(context.TODO(), c.roleBinding.Name, metav1.GetOptions{})
					if err != nil {
						t.Fatal(err)
					}
					assertFinalizers(t, rolebinding, c.expectedRoleBindingFinalizers)
				}

				if c.work != nil {
					work, err := fakeManifestWorkClient.WorkV1().ManifestWorks(c.work.Namespace).Get(context.TODO(), c.work.Name, metav1.GetOptions{})
					if err != nil {
						t.Fatal(err)
					}
					assertFinalizers(t, work, c.expectedWorkFinalizers)
				}

				actual := controllerContext.Queue().Len()
				if actual != c.expectedQueueLen {
					t.Errorf("Expect queue with length: %d, but got %d", c.expectedQueueLen, actual)
				}
			}()
		})
	}
}

func assertFinalizers(t *testing.T, obj runtime.Object, finalizers []string) {
	accessor, _ := meta.Accessor(obj)
	actual := accessor.GetFinalizers()
	if len(actual) == 0 && len(finalizers) == 0 {
		return
	}

	if !reflect.DeepEqual(actual, finalizers) {
		t.Fatal(diff.ObjectDiff(actual, finalizers))
	}
}

func noAction(t *testing.T, actions []clienttesting.Action) {
	if len(actions) > 0 {
		t.Fatal(spew.Sdump(actions))
	}
}
