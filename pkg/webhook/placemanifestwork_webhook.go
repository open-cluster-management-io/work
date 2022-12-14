package webhook

import (
	//"context"
	"encoding/json"
	//"fmt"
	"net/http"
	//"reflect"

	//ocmfeature "open-cluster-management.io/api/feature"
	workv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	//authenticationv1 "k8s.io/api/authentication/v1"
	//authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	//utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// ManifestWorkAdmissionHook will validate the creating/updating manifestwork request.
type PlaceManifestWorkAdmissionHook struct {
	kubeClient kubernetes.Interface
}

// ValidatingResource is called by generic-admission-server on startup to register the returned REST resource through which the
// webhook is accessed by the kube apiserver.
func (a *PlaceManifestWorkAdmissionHook) ValidatingResource() (plural schema.GroupVersionResource, singular string) {
	return schema.GroupVersionResource{
			Group:    "admission.work.open-cluster-management.io",
			Version:  "v1",
			Resource: "placemanifestworkvalidators",
		},
		"manifestworkvalidators"
}

// Validate is called by generic-admission-server when the registered REST resource above is called with an admission request.
func (a *PlaceManifestWorkAdmissionHook) Validate(admissionSpec *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	klog.V(4).Infof("validate %q operation for object %q", admissionSpec.Operation, admissionSpec.Object)

	status := &admissionv1beta1.AdmissionResponse{}

	// only validate the request for manifestwork
	if admissionSpec.Resource.Group != "work.open-cluster-management.io" ||
		admissionSpec.Resource.Resource != "placemanifestworks" {
		status.Allowed = true
		return status
	}

	switch admissionSpec.Operation {
	case admissionv1beta1.Create:
		return a.validateRequest(admissionSpec)
	case admissionv1beta1.Update:
		return a.validateRequest(admissionSpec)
	default:
		status.Allowed = true
		return status
	}
}

// Initialize is called by generic-admission-server on startup to setup initialization that placeManifestwork webhook needs.
func (a *PlaceManifestWorkAdmissionHook) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	var err error
	a.kubeClient, err = kubernetes.NewForConfig(kubeClientConfig)
	return err
}

// validateRequest validates creating/updating placeManifestwork operation
func (a *PlaceManifestWorkAdmissionHook) validateRequest(request *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	status := &admissionv1beta1.AdmissionResponse{}

	err := a.validatePlaceManifestWorkObj(request)
	if err != nil {
		status.Allowed = false
		status.Result = &metav1.Status{
			Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
			Message: err.Error(),
		}
		return status
	}

	status.Allowed = true
	return status
}

func (a *PlaceManifestWorkAdmissionHook) validatePlaceManifestWorkObj(request *admissionv1beta1.AdmissionRequest) error {
	placeWork := &workv1alpha1.PlaceManifestWork{}
	if err := json.Unmarshal(request.Object.Raw, placeWork); err != nil {
		return err
	}

	return placeWork
}
