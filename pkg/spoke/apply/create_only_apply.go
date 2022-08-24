package apply

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/helper"
)

type CreateOnlyApply struct {
	client dynamic.Interface
}

func NewCreateOnlyApply(client dynamic.Interface) *CreateOnlyApply {
	return &CreateOnlyApply{client: client}
}

func (c *CreateOnlyApply) Apply(ctx context.Context,
	gvr schema.GroupVersionResource,
	required *unstructured.Unstructured,
	owner metav1.OwnerReference,
	applyOption *workapiv1.ManifestConfigOption,
	recorder events.Recorder) Result {
	result := Result{}

	result.Result, result.Error = c.client.
		Resource(gvr).
		Namespace(required.GetNamespace()).
		Get(ctx, required.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(result.Error) {
		required.SetOwnerReferences([]metav1.OwnerReference{owner})
		result.Result, result.Error = c.client.Resource(gvr).Namespace(required.GetNamespace()).Create(
			ctx, resourcemerge.WithCleanLabelsAndAnnotations(required).(*unstructured.Unstructured), metav1.CreateOptions{})
		recorder.Eventf(fmt.Sprintf(
			"%s Created", required.GetKind()), "Created %s/%s because it was missing", required.GetNamespace(), required.GetName())
		return result
	}

	if result.Error != nil {
		return result
	}

	result.Error = helper.ApplyOwnerReferences(ctx, c.client, gvr, result.Result, owner)
	return result
}
