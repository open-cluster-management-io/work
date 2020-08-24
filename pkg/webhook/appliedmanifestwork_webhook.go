package webhook

import (
	"encoding/json"
	"fmt"
	"net/http"

	workv1 "github.com/open-cluster-management/api/work/v1"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// AppliedManifestWorkAdmissionHook will validate the creating of appliedmanifestwork request.
type AppliedManifestWorkAdmissionHook struct{}

// ValidatingResource is called by generic-admission-server on startup to register the returned REST resource through which the
// webhook is accessed by the kube apiserver.
func (a *AppliedManifestWorkAdmissionHook) ValidatingResource() (plural schema.GroupVersionResource, singular string) {
	return schema.GroupVersionResource{
			Group:    "admission.work.open-cluster-management.io",
			Version:  "v1",
			Resource: "appliedmanifestworkvalidators",
		},
		"appliedmanifestworkvalidators"
}

// Validate is called by generic-admission-server when the registered REST resource above is called with an admission request.
func (a *AppliedManifestWorkAdmissionHook) Validate(admissionSpec *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	klog.V(4).Infof("validate %q operation for object %q", admissionSpec.Operation, admissionSpec.Object)

	status := &admissionv1beta1.AdmissionResponse{}

	// only validate the request for manifestwork
	if admissionSpec.Resource.Group != "work.open-cluster-management.io" ||
		admissionSpec.Resource.Resource != "appliedmanifestworks" {
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

// Initialize is called by generic-admission-server on startup to setup initialization that appliedmanifestworks webhook needs.
func (a *AppliedManifestWorkAdmissionHook) Initialize(kubeClientConfig *rest.Config, stopCh <-chan struct{}) error {
	return nil
}

// validateRequest validates creating/updating appliedmanifestworks operation
func (a *AppliedManifestWorkAdmissionHook) validateRequest(request *admissionv1beta1.AdmissionRequest) *admissionv1beta1.AdmissionResponse {
	status := &admissionv1beta1.AdmissionResponse{}

	err := a.validateAppliedManifestWorkObj(request.Object)
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

// validateAppliedManifestWorkObj validates the fileds of AppliedManifestWork object
func (a *AppliedManifestWorkAdmissionHook) validateAppliedManifestWorkObj(requestObj runtime.RawExtension) error {
	appliedWork := &workv1.AppliedManifestWork{}
	if err := json.Unmarshal(requestObj.Raw, appliedWork); err != nil {
		return err
	}

	// Validate the name of the appliedmanifestwork
	if appliedWork.Name != fmt.Sprintf("%s-%s", appliedWork.Spec.HubHash, appliedWork.Spec.ManifestWorkName) {
		return fmt.Errorf("invalid appliedmanifestwork name")
	}

	return nil
}
