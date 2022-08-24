package apply

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/utils/pointer"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/helper"
)

type ServerSideApply struct {
	client dynamic.Interface
}

type ServerSideApplyConflictError struct {
	ssaErr error
}

func (e *ServerSideApplyConflictError) Error() string {
	return e.ssaErr.Error()
}

func NewServerSideApply(client dynamic.Interface) *ServerSideApply {
	return &ServerSideApply{client: client}
}

func (c *ServerSideApply) Apply(
	ctx context.Context,
	gvr schema.GroupVersionResource,
	required *unstructured.Unstructured,
	owner metav1.OwnerReference,
	applyOption *workapiv1.ManifestConfigOption,
	recorder events.Recorder) Result {

	force := false
	fieldManager := workapiv1.DefaultFieldManager
	result := Result{}

	if applyOption.UpdateStrategy.ServerSideApply != nil {
		force = applyOption.UpdateStrategy.ServerSideApply.Force
		if len(applyOption.UpdateStrategy.ServerSideApply.FieldManager) > 0 {
			fieldManager = applyOption.UpdateStrategy.ServerSideApply.FieldManager
		}
	}

	patch, err := json.Marshal(required)
	if err != nil {
		result.Error = err
		return result
	}

	// TODO use Apply method instead when upgrading the client-go to 0.25.x
	result.Result, result.Error = c.client.
		Resource(gvr).
		Namespace(required.GetNamespace()).
		Patch(ctx, required.GetName(), types.ApplyPatchType, patch, metav1.PatchOptions{FieldManager: fieldManager, Force: pointer.Bool(force)})
	resourceKey, _ := cache.MetaNamespaceKeyFunc(required)
	recorder.Eventf(fmt.Sprintf(
		"Server Side Applied %s %s", required.GetKind(), resourceKey), "Patched with field manager %s", fieldManager)

	if errors.IsConflict(result.Error) {
		result.Error = &ServerSideApplyConflictError{ssaErr: err}
	}

	if result.Error == nil {
		result.Error = helper.ApplyOwnerReferences(ctx, c.client, gvr, result.Result, owner)
	}

	return result

}
