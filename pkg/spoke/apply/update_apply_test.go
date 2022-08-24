package apply

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	clienttesting "k8s.io/client-go/testing"
	"open-cluster-management.io/work/pkg/spoke/spoketesting"
)

// Test unstructured compare
func TestIsSameUnstructured(t *testing.T) {
	cases := []struct {
		name     string
		obj1     *unstructured.Unstructured
		obj2     *unstructured.Unstructured
		expected bool
	}{
		{
			name:     "different kind",
			obj1:     spoketesting.NewUnstructured("v1", "Kind1", "ns1", "n1"),
			obj2:     spoketesting.NewUnstructured("v1", "Kind2", "ns1", "n1"),
			expected: false,
		},
		{
			name:     "different namespace",
			obj1:     spoketesting.NewUnstructured("v1", "Kind1", "ns1", "n1"),
			obj2:     spoketesting.NewUnstructured("v1", "Kind1", "ns2", "n1"),
			expected: false,
		},
		{
			name:     "different name",
			obj1:     spoketesting.NewUnstructured("v1", "Kind1", "ns1", "n1"),
			obj2:     spoketesting.NewUnstructured("v1", "Kind1", "ns1", "n2"),
			expected: false,
		},
		{
			name:     "different spec",
			obj1:     spoketesting.NewUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}}),
			obj2:     spoketesting.NewUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val2"}}),
			expected: false,
		},
		{
			name:     "same spec, different status",
			obj1:     spoketesting.NewUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}, "status": "status1"}),
			obj2:     spoketesting.NewUnstructuredWithContent("v1", "Kind1", "ns1", "n1", map[string]interface{}{"spec": map[string]interface{}{"key1": "val1"}, "status": "status2"}),
			expected: true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			actual := isSameUnstructured(c.obj1, c.obj2)
			if c.expected != actual {
				t.Errorf("expected %t, but %t", c.expected, actual)
			}
		})
	}
}

func TestApplyUnstructred(t *testing.T) {
	cases := []struct {
		name            string
		owner           metav1.OwnerReference
		existing        *unstructured.Unstructured
		required        *unstructured.Unstructured
		gvr             schema.GroupVersionResource
		validateActions func(t *testing.T, actions []clienttesting.Action)
	}{
		{
			name:     "create a new object with owner",
			owner:    metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: "testowner"},
			required: spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:      schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("Expect 2 actions, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "get")
				spoketesting.AssertAction(t, actions[1], "create")

				obj := actions[1].(clienttesting.CreateActionImpl).Object.(*unstructured.Unstructured)
				owners := obj.GetOwnerReferences()
				if len(owners) != 1 {
					t.Errorf("Expect 1 owners, but have %d", len(owners))
				}

				if owners[0].UID != "testowner" {
					t.Errorf("Owner UId is not correct, got %s", owners[0].UID)
				}
			},
		},
		{
			name:     "create a new object without owner",
			owner:    metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: "testowner-"},
			required: spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:      schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("Expect 2 actions, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "get")
				spoketesting.AssertAction(t, actions[1], "create")

				obj := actions[1].(clienttesting.CreateActionImpl).Object.(*unstructured.Unstructured)
				owners := obj.GetOwnerReferences()
				if len(owners) != 0 {
					t.Errorf("Expect 1 owners, but have %d", len(owners))
				}
			},
		},
		{
			name: "update an object owner",
			existing: spoketesting.NewUnstructured(
				"v1", "Secret", "ns1", "test", metav1.OwnerReference{APIVersion: "v1", Name: "test1", UID: "testowner1"}),
			owner:    metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: "testowner"},
			required: spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:      schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("Expect 2 actions, but have %d", len(actions))
				}

				spoketesting.AssertAction(t, actions[0], "get")
				spoketesting.AssertAction(t, actions[1], "update")

				obj := actions[1].(clienttesting.UpdateActionImpl).Object.(*unstructured.Unstructured)
				owners := obj.GetOwnerReferences()
				if len(owners) != 2 {
					t.Errorf("Expect 2 owners, but have %d", len(owners))
				}

				if owners[0].UID != "testowner1" {
					t.Errorf("Owner UId is not correct, got %s", owners[0].UID)
				}
				if owners[1].UID != "testowner" {
					t.Errorf("Owner UId is not correct, got %s", owners[1].UID)
				}
			},
		},
		{
			name: "update an object without owner",
			existing: spoketesting.NewUnstructured(
				"v1", "Secret", "ns1", "test", metav1.OwnerReference{APIVersion: "v1", Name: "test1", UID: "testowner1"}),
			owner:    metav1.OwnerReference{Name: "test", UID: "testowner-"},
			required: spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:      schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("Expect 1 actions, but have %d", len(actions))
				}
			},
		},
		{
			name: "remove an object owner",
			existing: spoketesting.NewUnstructured(
				"v1", "Secret", "ns1", "test", metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: "testowner"}),
			owner:    metav1.OwnerReference{APIVersion: "v1", Name: "test", UID: "testowner-"},
			required: spoketesting.NewUnstructured("v1", "Secret", "ns1", "test"),
			gvr:      schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("Expect 2 actions, but have %d", len(actions))
				}
				spoketesting.AssertAction(t, actions[0], "get")
				spoketesting.AssertAction(t, actions[1], "update")

				obj := actions[1].(clienttesting.UpdateActionImpl).Object.(*unstructured.Unstructured)
				owners := obj.GetOwnerReferences()
				if len(owners) != 0 {
					t.Errorf("Expect 0 owner, but have %d", len(owners))
				}
			},
		},
		{
			name: "merge labels",
			existing: func() *unstructured.Unstructured {
				obj := spoketesting.NewUnstructured(
					"v1", "Secret", "ns1", "test", metav1.OwnerReference{APIVersion: "v1", Name: "test1", UID: "testowner1"})
				obj.SetLabels(map[string]string{"foo": "bar"})
				return obj
			}(),
			owner: metav1.OwnerReference{APIVersion: "v1", Name: "test1", UID: "testowner1"},
			required: func() *unstructured.Unstructured {
				obj := spoketesting.NewUnstructured(
					"v1", "Secret", "ns1", "test", metav1.OwnerReference{APIVersion: "v1", Name: "test1", UID: "testowner1"})
				obj.SetLabels(map[string]string{"foo1": "bar1"})
				return obj
			}(),
			gvr: schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("Expect 2 actions, but have %d", len(actions))
				}
				spoketesting.AssertAction(t, actions[0], "get")
				spoketesting.AssertAction(t, actions[1], "update")

				obj := actions[1].(clienttesting.UpdateActionImpl).Object.(*unstructured.Unstructured)
				labels := obj.GetLabels()
				if len(labels) != 2 {
					t.Errorf("Expect 2 labels, but have %d", len(labels))
				}
			},
		},
		{
			name: "merge annotation",
			existing: func() *unstructured.Unstructured {
				obj := spoketesting.NewUnstructured(
					"v1", "Secret", "ns1", "test", metav1.OwnerReference{APIVersion: "v1", Name: "test1", UID: "testowner1"})
				obj.SetAnnotations(map[string]string{"foo": "bar"})
				return obj
			}(),
			owner: metav1.OwnerReference{APIVersion: "v1", Name: "test1", UID: "testowner1"},
			required: func() *unstructured.Unstructured {
				obj := spoketesting.NewUnstructured(
					"v1", "Secret", "ns1", "test", metav1.OwnerReference{APIVersion: "v1", Name: "test1", UID: "testowner1"})
				obj.SetAnnotations(map[string]string{"foo1": "bar1"})
				return obj
			}(),
			gvr: schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 2 {
					t.Errorf("Expect 2 actions, but have %d", len(actions))
				}
				spoketesting.AssertAction(t, actions[0], "get")
				spoketesting.AssertAction(t, actions[1], "update")

				obj := actions[1].(clienttesting.UpdateActionImpl).Object.(*unstructured.Unstructured)
				annotations := obj.GetAnnotations()
				if len(annotations) != 2 {
					t.Errorf("Expect 2 annotations, but have %d", len(annotations))
				}
			},
		},
		{
			name: "set existing finalizer",
			existing: func() *unstructured.Unstructured {
				obj := spoketesting.NewUnstructured(
					"v1", "Secret", "ns1", "test", metav1.OwnerReference{APIVersion: "v1", Name: "test1", UID: "testowner1"})
				obj.SetFinalizers([]string{"foo"})
				return obj
			}(),
			owner: metav1.OwnerReference{APIVersion: "v1", Name: "test1", UID: "testowner1"},
			required: func() *unstructured.Unstructured {
				obj := spoketesting.NewUnstructured(
					"v1", "Secret", "ns1", "test", metav1.OwnerReference{APIVersion: "v1", Name: "test1", UID: "testowner1"})
				obj.SetFinalizers([]string{"foo1"})
				return obj
			}(),
			gvr: schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("Expect 1 actions, but have %d", len(actions))
				}
			},
		},
		{
			name: "nothing to update",
			existing: func() *unstructured.Unstructured {
				obj := spoketesting.NewUnstructured(
					"v1", "Secret", "ns1", "test", metav1.OwnerReference{APIVersion: "v1", Name: "test1", UID: "testowner1"})
				obj.SetLabels(map[string]string{"foo": "bar"})
				obj.SetAnnotations(map[string]string{"foo": "bar"})
				return obj
			}(),
			owner: metav1.OwnerReference{APIVersion: "v1", Name: "test1", UID: "testowner1"},
			required: func() *unstructured.Unstructured {
				obj := spoketesting.NewUnstructured(
					"v1", "Secret", "ns1", "test", metav1.OwnerReference{APIVersion: "v1", Name: "test1", UID: "testowner1"})
				obj.SetLabels(map[string]string{"foo": "bar"})
				obj.SetAnnotations(map[string]string{"foo": "bar"})
				return obj
			}(),
			gvr: schema.GroupVersionResource{Version: "v1", Resource: "secrets"},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("Expect 1 actions, but have %v", actions)
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			objects := []runtime.Object{}
			if c.existing != nil {
				objects = append(objects, c.existing)
			}
			scheme := runtime.NewScheme()
			dynamicClient := fakedynamic.NewSimpleDynamicClient(scheme, objects...)
			applier := NewUpdateApply(dynamicClient, nil, nil)

			c.required.SetOwnerReferences([]metav1.OwnerReference{c.owner})
			syncContext := spoketesting.NewFakeSyncContext(t, "test")
			_, _, err := applier.applyUnstructured(
				context.TODO(), c.required, c.gvr, syncContext.Recorder())

			if err != nil {
				t.Errorf("expect no error, but got %v", err)
			}

			c.validateActions(t, dynamicClient.Actions())
		})
	}
}
