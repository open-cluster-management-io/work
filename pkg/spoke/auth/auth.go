package auth

import (
	"context"
	"fmt"
	"strings"

	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"

	workapiv1 "open-cluster-management.io/api/work/v1"
)

// ExecuteAction is the action of executing the manifest work
type ExecuteAction string

const (
	// ApplyAction represents applying(create/update) resource to the managed cluster
	ApplyAction ExecuteAction = "Apply"
	// DeleteAction represents deleting resource from the managed cluster
	DeleteAction ExecuteAction = "Delete"
)

// ExecutorValidator validates whether the executor has permission to perform the requests
// to the local managed cluster
type ExecutorValidator interface {
	// Validate whether the work executor subject has permission to perform action on the specific manifest,
	// if there is no permission will return a kubernetes forbidden error.
	Validate(ctx context.Context, executor *workapiv1.ManifestWorkExecutor,
		gvr schema.GroupVersionResource, namespace, name string, action ExecuteAction) error
}

func NewExecutorValidator(kubeClient kubernetes.Interface) ExecutorValidator {
	return &sarValidator{
		kubeClient: kubeClient,
	}
}

type sarValidator struct {
	kubeClient kubernetes.Interface
}

func (v *sarValidator) Validate(ctx context.Context, executor *workapiv1.ManifestWorkExecutor,
	gvr schema.GroupVersionResource, namespace, name string, action ExecuteAction) error {
	if executor == nil {
		return nil
	}

	if executor.Subject.Type != workapiv1.ExecutorSubjectTypeServiceAccount {
		return fmt.Errorf("only support %s type for the executor", workapiv1.ExecutorSubjectTypeServiceAccount)
	}

	sa := executor.Subject.ServiceAccount
	if sa == nil {
		return fmt.Errorf("the executor service account is nil")
	}

	var verbs []string
	switch action {
	case ApplyAction:
		verbs = []string{"create", "update", "patch", "get", "list"}
	case DeleteAction:
		verbs = []string{"delete"}
	default:
		return fmt.Errorf("execute action %s is invalid", action)
	}

	resource := authorizationv1.ResourceAttributes{
		Namespace: namespace,
		Name:      name,
		Group:     gvr.Group,
		Version:   gvr.Version,
		Resource:  gvr.Resource,
	}

	reviews := buildSubjectAccessReviews(sa.Namespace, sa.Name, resource, verbs...)
	allowed, err := validateBySubjectAccessReviews(ctx, v.kubeClient, reviews)
	if err != nil {
		return err
	}

	if !allowed {
		return errors.NewForbidden(schema.GroupResource{
			Group:    resource.Group,
			Resource: resource.Resource,
		}, resource.Name, fmt.Errorf("not allowed to %s the resource", strings.ToLower(string(action))))
	}

	return nil
}

func buildSubjectAccessReviews(saNamespace string, saName string,
	resource authorizationv1.ResourceAttributes,
	verbs ...string) []authorizationv1.SubjectAccessReview {

	reviews := []authorizationv1.SubjectAccessReview{}
	for _, verb := range verbs {
		reviews = append(reviews, authorizationv1.SubjectAccessReview{
			Spec: authorizationv1.SubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Group:       resource.Group,
					Resource:    resource.Resource,
					Version:     resource.Version,
					Subresource: resource.Subresource,
					Name:        resource.Name,
					Namespace:   resource.Namespace,
					Verb:        verb,
				},
				User: fmt.Sprintf("system:serviceaccount:%s:%s", saNamespace, saName),
				Groups: []string{"system:serviceaccounts", "system:authenticated",
					fmt.Sprintf("system:serviceaccounts:%s", saNamespace)},
			},
		})
	}
	return reviews
}

func validateBySubjectAccessReviews(
	ctx context.Context,
	kubeClient kubernetes.Interface,
	subjectAccessReviews []authorizationv1.SubjectAccessReview) (bool, error) {

	for i := range subjectAccessReviews {
		subjectAccessReview := subjectAccessReviews[i]

		sar, err := kubeClient.AuthorizationV1().SubjectAccessReviews().Create(
			ctx, &subjectAccessReview, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}
		if !sar.Status.Allowed {
			return false, nil
		}
	}
	return true, nil
}
