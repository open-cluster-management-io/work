package webhook

import (
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/open-cluster-management/work/pkg/spoke/spoketesting"

	admissionv1beta1 "k8s.io/api/admission/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var appliedManifestWorkSchema = metav1.GroupVersionResource{
	Group:    "work.open-cluster-management.io",
	Version:  "v1",
	Resource: "appliedmanifestworks",
}

func TestAppliedManifestWorkValidate(t *testing.T) {
	cases := []struct {
		name                    string
		request                 *admissionv1beta1.AdmissionRequest
		hubHash                 string
		appliedManifestWorkName string
		expectedResponse        *admissionv1beta1.AdmissionResponse
	}{
		{
			name: "validate non-manifestwork request",
			request: &admissionv1beta1.AdmissionRequest{
				Resource: metav1.GroupVersionResource{
					Group:    "test.open-cluster-management.io",
					Version:  "v1",
					Resource: "tests",
				},
			},
			hubHash:                 "test",
			appliedManifestWorkName: "test-work-0",
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate creating AppliedManaifestWork",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  appliedManifestWorkSchema,
				Operation: admissionv1beta1.Create,
			},
			hubHash:                 "test",
			appliedManifestWorkName: "test-work-0",
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: true,
			},
		},
		{
			name: "validate creating AppliedManaifestWork with invalid name",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  appliedManifestWorkSchema,
				Operation: admissionv1beta1.Create,
			},
			hubHash:                 "test",
			appliedManifestWorkName: "test1-work-0",
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: "invalid appliedmanifestwork name",
				},
			},
		},
		{
			name: "validate updating AppliedManaifestWork with invalid name",
			request: &admissionv1beta1.AdmissionRequest{
				Resource:  appliedManifestWorkSchema,
				Operation: admissionv1beta1.Update,
			},
			hubHash:                 "test",
			appliedManifestWorkName: "test-work-1",
			expectedResponse: &admissionv1beta1.AdmissionResponse{
				Allowed: false,
				Result: &metav1.Status{
					Status: metav1.StatusFailure, Code: http.StatusBadRequest, Reason: metav1.StatusReasonBadRequest,
					Message: "invalid appliedmanifestwork name",
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			appliedWork := spoketesting.NewAppliedManifestWork(c.hubHash, 0)
			appliedWork.Name = c.appliedManifestWorkName
			c.request.Object.Raw, _ = json.Marshal(appliedWork)
			admissionHook := &AppliedManifestWorkAdmissionHook{}
			actualResponse := admissionHook.Validate(c.request)
			if !reflect.DeepEqual(actualResponse, c.expectedResponse) {
				t.Errorf("expected %#v but got: %#v", c.expectedResponse.Result, actualResponse.Result)
			}
		})
	}
}
