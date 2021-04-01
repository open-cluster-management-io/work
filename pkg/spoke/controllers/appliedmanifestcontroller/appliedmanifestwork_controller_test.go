package appliedmanifestcontroller

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/davecgh/go-spew/spew"
	fakeworkclient "github.com/open-cluster-management/api/client/work/clientset/versioned/fake"
	workinformers "github.com/open-cluster-management/api/client/work/informers/externalversions"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/open-cluster-management/work/pkg/spoke/spoketesting"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/diff"
)

func newManifest(group, version, resource, namespace, name string) workapiv1.ManifestCondition {
	return workapiv1.ManifestCondition{
		ResourceMeta: workapiv1.ManifestResourceMeta{
			Group:     group,
			Version:   version,
			Resource:  resource,
			Namespace: namespace,
			Name:      name,
		},
	}
}

func TestSyncManifestWork(t *testing.T) {
	testingAppliedWork := spoketesting.NewAppliedManifestWork("test", 0)
	owner := metav1.NewControllerRef(testingAppliedWork, workapiv1.GroupVersion.WithKind("AppliedManifestWork"))

	// Create another owner for other work
	anotherOwner := metav1.NewControllerRef(testingAppliedWork, workapiv1.GroupVersion.WithKind("AppliedManifestWork"))
	anotherOwner.UID = "anotheruid"

	cases := []struct {
		name                               string
		existingResources                  []runtime.Object
		appliedResources                   []workapiv1.AppliedManifestResourceMeta
		manifests                          []workapiv1.ManifestCondition
		validateAppliedManifestWorkActions func(t *testing.T, actions []clienttesting.Action)
		expectedDeleteActions              []clienttesting.DeleteActionImpl
		expectedUpdateActionFunc           func(t *testing.T, actions []clienttesting.UpdateActionImpl)
		expectedQueueLen                   int
	}{
		{
			name: "skip when no applied resource changed",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecretWithOwner("ns1", "n1", false, "ns1-n1", []metav1.OwnerReference{*owner}),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1", UID: "ns1-n1"},
			},
			manifests: []workapiv1.ManifestCondition{newManifest("", "v1", "secrets", "ns1", "n1")},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) > 0 {
					t.Fatal(spew.Sdump(actions))
				}
			},
			expectedDeleteActions: []clienttesting.DeleteActionImpl{},
		},
		{
			name: "delete untracked resources",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecretWithOwner("ns1", "n1", false, "ns1-n1", []metav1.OwnerReference{*owner}),
				spoketesting.NewUnstructuredSecretWithOwner("ns2", "n2", false, "ns2-n2", []metav1.OwnerReference{*owner}),
				spoketesting.NewUnstructuredSecretWithOwner("ns3", "n3", false, "ns3-n3", []metav1.OwnerReference{*owner}),
				spoketesting.NewUnstructuredSecretWithOwner("ns4", "n4", false, "ns4-n4", []metav1.OwnerReference{*owner}),
				spoketesting.NewUnstructuredSecretWithOwner("ns5", "n5", false, "ns5-n5", []metav1.OwnerReference{*owner}),
				spoketesting.NewUnstructuredSecretWithOwner("ns6", "n6", false, "ns6-n6", []metav1.OwnerReference{*owner}),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "", Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1", UID: "ns1-n1"},
				{Group: "", Version: "v1", Resource: "secrets", Namespace: "ns2", Name: "n2", UID: "ns2-n2"},
				{Group: "", Version: "v1", Resource: "secrets", Namespace: "ns3", Name: "n3", UID: "ns3-n3"},
				{Group: "", Version: "v1", Resource: "secrets", Namespace: "ns4", Name: "n4", UID: "ns4-n4"},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
				newManifest("", "v1", "secrets", "ns2", "n2"),
				newManifest("", "v1", "secrets", "ns5", "n5"),
				newManifest("", "v1", "secrets", "ns6", "n6"),
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.AppliedManifestWork)
				if !reflect.DeepEqual(work.Status.AppliedResources, []workapiv1.AppliedManifestResourceMeta{
					{Group: "", Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1", UID: "ns1-n1"},
					{Group: "", Version: "v1", Resource: "secrets", Namespace: "ns2", Name: "n2", UID: "ns2-n2"},
					{Group: "", Version: "v1", Resource: "secrets", Namespace: "ns3", Name: "n3", UID: "ns3-n3"},
					{Group: "", Version: "v1", Resource: "secrets", Namespace: "ns4", Name: "n4", UID: "ns4-n4"},
					{Group: "", Version: "v1", Resource: "secrets", Namespace: "ns5", Name: "n5", UID: "ns5-n5"},
					{Group: "", Version: "v1", Resource: "secrets", Namespace: "ns6", Name: "n6", UID: "ns6-n6"},
				}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
			expectedDeleteActions: []clienttesting.DeleteActionImpl{
				clienttesting.NewDeleteAction(schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, "ns3", "n3"),
				clienttesting.NewDeleteAction(schema.GroupVersionResource{Group: "", Version: "v1", Resource: "secrets"}, "ns4", "n4"),
			},
		},
		{
			name: "requeue work when applied resource for stale manifest is deleting",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecretWithOwner("ns1", "n1", false, "ns1-n1", []metav1.OwnerReference{*owner}),
				spoketesting.NewUnstructuredSecretWithOwner("ns2", "n2", false, "ns2-n2", []metav1.OwnerReference{*owner}),
				spoketesting.NewUnstructuredSecretWithOwner("ns3", "n3", true, "ns3-n3", []metav1.OwnerReference{*owner}),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1", UID: "ns1-n1"},
				{Version: "v1", Resource: "secrets", Namespace: "ns2", Name: "n2", UID: "ns2-n2"},
				{Version: "v1", Resource: "secrets", Namespace: "ns3", Name: "n3", UID: "ns3-n3"},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
				newManifest("", "v1", "secrets", "ns2", "n2"),
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) > 0 {
					t.Fatal(spew.Sdump(actions))
				}
			},
			expectedDeleteActions: []clienttesting.DeleteActionImpl{},
			expectedQueueLen:      1,
		},
		{
			name: "ignore re-created resource",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecretWithOwner("ns3", "n3", false, "ns3-n3-recreated", []metav1.OwnerReference{*owner}),
				spoketesting.NewUnstructuredSecretWithOwner("ns1", "n1", false, "ns1-n1", []metav1.OwnerReference{*owner}),
				spoketesting.NewUnstructuredSecretWithOwner("ns5", "n5", false, "ns5-n5", []metav1.OwnerReference{*owner}),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", Resource: "secrets", Namespace: "ns3", Name: "n3", UID: "ns3-n3"},
				{Version: "v1", Resource: "secrets", Namespace: "ns4", Name: "n4", UID: "ns4-n4"},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
				newManifest("", "v1", "secrets", "ns5", "n5"),
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.AppliedManifestWork)
				if !reflect.DeepEqual(work.Status.AppliedResources, []workapiv1.AppliedManifestResourceMeta{
					{Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1", UID: "ns1-n1"},
					{Version: "v1", Resource: "secrets", Namespace: "ns5", Name: "n5", UID: "ns5-n5"},
				}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
			expectedDeleteActions: []clienttesting.DeleteActionImpl{},
		},
		{
			name: "update resource uid",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecretWithOwner("ns1", "n1", false, "ns1-n1", []metav1.OwnerReference{*owner}),
				spoketesting.NewUnstructuredSecretWithOwner("ns2", "n2", false, "ns2-n2-updated", []metav1.OwnerReference{*owner}),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1", UID: "ns1-n1"},
				{Version: "v1", Resource: "secrets", Namespace: "ns2", Name: "n2", UID: "ns2-n2"},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns1", "n1"),
				newManifest("", "v1", "secrets", "ns2", "n2"),
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.AppliedManifestWork)
				if !reflect.DeepEqual(work.Status.AppliedResources, []workapiv1.AppliedManifestResourceMeta{
					{Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1", UID: "ns1-n1"},
					{Version: "v1", Resource: "secrets", Namespace: "ns2", Name: "n2", UID: "ns2-n2-updated"},
				}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
			expectedDeleteActions: []clienttesting.DeleteActionImpl{},
		},
		{
			name: "remove owner only when resource is removed frome appiedmanifestwork",
			existingResources: []runtime.Object{
				spoketesting.NewUnstructuredSecretWithOwner("ns1", "n1", false, "ns1-n1", []metav1.OwnerReference{*owner, *anotherOwner}),
				spoketesting.NewUnstructuredSecretWithOwner("ns2", "n2", false, "ns2-n2", []metav1.OwnerReference{*owner}),
			},
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Version: "v1", Resource: "secrets", Namespace: "ns1", Name: "n1", UID: "ns1-n1"},
				{Version: "v1", Resource: "secrets", Namespace: "ns2", Name: "n2", UID: "ns2-n2"},
			},
			manifests: []workapiv1.ManifestCondition{
				newManifest("", "v1", "secrets", "ns2", "n2"),
			},
			validateAppliedManifestWorkActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
				work := actions[0].(clienttesting.UpdateAction).GetObject().(*workapiv1.AppliedManifestWork)
				if !reflect.DeepEqual(work.Status.AppliedResources, []workapiv1.AppliedManifestResourceMeta{
					{Version: "v1", Resource: "secrets", Namespace: "ns2", Name: "n2", UID: "ns2-n2"},
				}) {
					t.Fatal(spew.Sdump(actions))
				}
			},
			expectedDeleteActions: []clienttesting.DeleteActionImpl{},
			expectedUpdateActionFunc: func(t *testing.T, actions []clienttesting.UpdateActionImpl) {
				if len(actions) != 1 {
					t.Fatal(spew.Sdump(actions))
				}

				obj := actions[0].Object
				accessor, _ := meta.Accessor(obj)
				if len(accessor.GetOwnerReferences()) != 1 {
					t.Fatal(spew.Sdump(actions))
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testingWork, _ := spoketesting.NewManifestWork(0)
			testingAppliedWork := spoketesting.NewAppliedManifestWork("test", 0)
			testingAppliedWork.Status.AppliedResources = c.appliedResources
			testingWork.Status.ResourceStatus.Manifests = c.manifests

			fakeDynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), c.existingResources...)
			fakeClient := fakeworkclient.NewSimpleClientset(testingWork, testingAppliedWork)
			informerFactory := workinformers.NewSharedInformerFactory(fakeClient, 5*time.Minute)
			informerFactory.Work().V1().ManifestWorks().Informer().GetStore().Add(testingWork)
			informerFactory.Work().V1().AppliedManifestWorks().Informer().GetStore().Add(testingAppliedWork)
			controller := AppliedManifestWorkController{
				manifestWorkClient:        fakeClient.WorkV1().ManifestWorks(testingWork.Namespace),
				manifestWorkLister:        informerFactory.Work().V1().ManifestWorks().Lister().ManifestWorks("cluster1"),
				appliedManifestWorkClient: fakeClient.WorkV1().AppliedManifestWorks(),
				appliedManifestWorkLister: informerFactory.Work().V1().AppliedManifestWorks().Lister(),
				spokeDynamicClient:        fakeDynamicClient,
				hubHash:                   "test",
				rateLimiter:               workqueue.NewItemExponentialFailureRateLimiter(0, 1*time.Second),
			}

			controllerContext := spoketesting.NewFakeSyncContext(t, testingWork.Name)
			err := controller.sync(context.TODO(), controllerContext)
			if err != nil {
				t.Fatal(err)
			}
			c.validateAppliedManifestWorkActions(t, fakeClient.Actions())

			deleteActions := []clienttesting.DeleteActionImpl{}
			for _, action := range fakeDynamicClient.Actions() {
				if action.GetVerb() == "delete" {
					deleteActions = append(deleteActions, action.(clienttesting.DeleteActionImpl))
				}
			}
			if !reflect.DeepEqual(c.expectedDeleteActions, deleteActions) {
				t.Fatal(spew.Sdump(deleteActions))
			}

			updateActions := []clienttesting.UpdateActionImpl{}
			for _, action := range fakeDynamicClient.Actions() {
				if action.GetVerb() == "update" {
					updateActions = append(updateActions, action.(clienttesting.UpdateActionImpl))
				}
			}
			if c.expectedUpdateActionFunc != nil {
				c.expectedUpdateActionFunc(t, updateActions)
			}

			queueLen := controllerContext.Queue().Len()
			if queueLen != c.expectedQueueLen {
				t.Errorf("expected %d, but %d", c.expectedQueueLen, queueLen)
			}
		})
	}

}

func TestFindUntrackedResources(t *testing.T) {
	cases := []struct {
		name                       string
		appliedResources           []workapiv1.AppliedManifestResourceMeta
		newAppliedResources        []workapiv1.AppliedManifestResourceMeta
		expectedUntrackedResources []workapiv1.AppliedManifestResourceMeta
	}{
		{
			name:             "no resource untracked",
			appliedResources: nil,
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g1", Version: "v1", Resource: "r1", Namespace: "ns1", Name: "n1"},
			},
			expectedUntrackedResources: nil,
		},
		{
			name: "some of original resources untracked",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g1", Version: "v1", Resource: "r1", Namespace: "ns1", Name: "n1"},
				{Group: "g2", Version: "v2", Resource: "r2", Namespace: "ns2", Name: "n2"},
			},
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g2", Version: "v2", Resource: "r2", Namespace: "ns2", Name: "n2"},
				{Group: "g3", Version: "v3", Resource: "r3", Namespace: "ns3", Name: "n3"},
			},
			expectedUntrackedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g1", Version: "v1", Resource: "r1", Namespace: "ns1", Name: "n1"},
			},
		},
		{
			name: "all original resources untracked",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g1", Version: "v1", Resource: "r1", Namespace: "ns1", Name: "n1"},
				{Group: "g2", Version: "v2", Resource: "r2", Namespace: "ns2", Name: "n2"},
			},
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g3", Version: "v3", Resource: "r3", Namespace: "ns3", Name: "n3"},
				{Group: "g4", Version: "v4", Resource: "r4", Namespace: "ns4", Name: "n4"},
			},
			expectedUntrackedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g1", Version: "v1", Resource: "r1", Namespace: "ns1", Name: "n1"},
				{Group: "g2", Version: "v2", Resource: "r2", Namespace: "ns2", Name: "n2"},
			},
		},
		{
			name: "changing version of original resources does not make it untracked",
			appliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g1", Version: "v1", Resource: "r1", Namespace: "ns1", Name: "n1"},
				{Group: "g2", Version: "v2", Resource: "r2", Namespace: "ns2", Name: "n2"},
			},
			newAppliedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g1", Version: "v2", Resource: "r1", Namespace: "ns1", Name: "n1"},
				{Group: "g4", Version: "v4", Resource: "r4", Namespace: "ns4", Name: "n4"},
			},
			expectedUntrackedResources: []workapiv1.AppliedManifestResourceMeta{
				{Group: "g2", Version: "v2", Resource: "r2", Namespace: "ns2", Name: "n2"},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := findUntrackedResources(c.appliedResources, c.newAppliedResources)
			if !reflect.DeepEqual(actual, c.expectedUntrackedResources) {
				t.Errorf(diff.ObjectDiff(actual, c.expectedUntrackedResources))
			}
		})
	}
}
