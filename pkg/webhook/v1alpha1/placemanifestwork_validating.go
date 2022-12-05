package v1alpha1

import (
	"context"
	"fmt"
	"reflect"

	//authenticationv1 "k8s.io/api/authentication/v1"
	//authorizationv1 "k8s.io/api/authorization/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	//metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/kubernetes"
	ocmfeature "open-cluster-management.io/api/feature"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ webhook.CustomValidator = &PlaceManifestWorkWebhook{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ManifestWorkWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	placeWork, ok := obj.(*workv1alpha1.PlaceManifestWork)
	if !ok {
		return apierrors.NewBadRequest("Request placeManifestwork obj format is not right")
	}
	return r.validateRequest(placeWork, nil, ctx)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *PlaceManifestWorkWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) error {
	newPlaceWork, ok := newObj.(*workv1alpha1.PlaceManifestWork)
	if !ok {
		return apierrors.NewBadRequest("Request placeManifestwork obj format is not right")
	}

	oldPlaceWork, ok := oldObj.(*workv1alpha1.PlaceManifestWork)
	if !ok {
		return apierrors.NewBadRequest("Request placeManifestwork obj format is not right")
	}

	return r.validateRequest(newPlaceWork, oldPlaceWork, ctx)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *PlaceManifestWorkWebhook) ValidateDelete(_ context.Context, obj runtime.Object) error {
	return nil
}

func (r *PlaceManifestWorkWebhook) validateRequest(newPlaceWork, oldPlaceWork *workv1alpha1.PlaceManifestWork, ctx context.Context) error {
	if len(newPlaceWork.Spec.ManifestWorkTemplate) == 0 {
		return apierrors.NewBadRequest("manifestWork should not be empty")
	}

	if err := validateManifests(newPlaceWork); err != nil {
		return apierrors.NewBadRequest(err.Error())
	}

	_, err := admission.RequestFromContext(ctx)
	if err != nil {
		return apierrors.NewBadRequest(err.Error())
	}

	return nil
}
