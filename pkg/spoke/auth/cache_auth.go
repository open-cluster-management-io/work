package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

// ExecuteAction is the action of executing the manifest work
type ExecuteAction string

const (
	// ApplyAndDeleteAction represents applying(create/update) resource to the managed cluster,
	// and is responsiable for deleting the resource
	ApplyAndDeleteAction ExecuteAction = "ApplyAndDelete"
	// ApplyNoDeleteAction represents only applying(create/update) resource to the managed cluster,
	// but is not responsiable for deleting the resource
	ApplyNoDeleteAction ExecuteAction = "ApplyNoDelete"
)

// SubjectAccessReviewCheckFn is a function to checks if the executor has permission to operate
// the gvr resource by subjectaccessreview
type SubjectAccessReviewCheckFn func(ctx context.Context, executor *workapiv1.ManifestWorkSubjectServiceAccount,
	gvr schema.GroupVersionResource, namespace, name string, ownedByTheWork bool) error

// GetSARCheckerFn returns the SubjectAccessReviewCheckFn
func (v *sarCacheValidator) GetSARCheckerFn() SubjectAccessReviewCheckFn {
	return v.validator.checkSubjectAccessReviews
}

// NewExecutorCacheValidator creates a sarCacheValidator
func NewExecutorCacheValidator(validator *sarValidator, executorCaches *ExecutorCaches) *sarCacheValidator {
	return &sarCacheValidator{
		validator:      validator,
		executorCaches: executorCaches,
	}
}

type sarCacheValidator struct {
	executorCaches *ExecutorCaches
	validator      *sarValidator
}

func GetExecuteAction(ownedByTheWork bool) ExecuteAction {
	if ownedByTheWork {
		return ApplyAndDeleteAction
	}
	return ApplyNoDeleteAction
}

func OwnedByWork(action ExecuteAction) bool {
	return action == ApplyAndDeleteAction
}

func (v *sarCacheValidator) Validate(ctx context.Context, executor *workapiv1.ManifestWorkExecutor,
	gvr schema.GroupVersionResource, namespace, name string,
	ownedByTheWork bool, obj *unstructured.Unstructured) error {
	if executor == nil {
		return nil
	}

	if err := v.validator.executorBasicCheck(executor); err != nil {
		return err
	}

	sa := executor.Subject.ServiceAccount
	executorKey := ExecutorKey(sa.Namespace, sa.Name)
	dimension := Dimension{
		Namespace:     namespace,
		Name:          name,
		Resource:      gvr.Resource,
		Group:         gvr.Group,
		Version:       gvr.Version,
		ExecuteAction: GetExecuteAction(ownedByTheWork),
	}

	if allow, ok := v.executorCaches.Get(executorKey, dimension); !ok {
		err := v.validator.checkSubjectAccessReviews(ctx, sa, gvr, namespace, name, ownedByTheWork)
		UpdateSARCheckResultToCache(v.executorCaches, executorKey, dimension, err)
		if err != nil {
			return err
		}
	} else {
		klog.V(5).Infof("Get auth from cache executor %s, dimension: %+v allow: %v", executorKey, dimension, allow)
		if !allow {
			return &NotAllowedError{
				Err: fmt.Errorf("not allowed to apply the resource %s %s, %s %s",
					gvr.Group, gvr.Resource, namespace, name),
				RequeueTime: 60 * time.Second,
			}
		}
	}

	return v.validator.checkEscalation(ctx, sa, gvr, namespace, name, obj)
}

// UpdateSARCheckResultToCache updates the subjectAccessReview checking result to the executor cache
func UpdateSARCheckResultToCache(executorCaches *ExecutorCaches, executorKey string,
	dimension Dimension, result error) {
	if result == nil {
		executorCaches.Add(executorKey, dimension, true)
	}

	var authError = &NotAllowedError{}
	if errors.As(result, &authError) {
		executorCaches.Add(executorKey, dimension, false)
	}
}
