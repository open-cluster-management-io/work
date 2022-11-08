package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/work/pkg/spoke/auth/basic"
	"open-cluster-management.io/work/pkg/spoke/auth/store"
)

// SubjectAccessReviewCheckFn is a function to checks if the executor has permission to operate
// the gvr resource by subjectaccessreview
type SubjectAccessReviewCheckFn func(ctx context.Context, executor *workapiv1.ManifestWorkSubjectServiceAccount,
	gvr schema.GroupVersionResource, namespace, name string, ownedByTheWork bool) error

// NewExecutorCacheValidator creates a sarCacheValidator
func NewExecutorCacheValidator(
	ctx context.Context,
	recorder events.Recorder,
	spokeKubeClient kubernetes.Interface,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	restMapper meta.RESTMapper,
	validator *basic.SarValidator,
) *sarCacheValidator {

	executorCaches := store.NewExecutorCache()

	// the spokeKubeInformerFactory will only be used for the executor cache controller, and we do not want to
	// update the cache very frequently, set resync period to every day
	spokeKubeInformerFactory := informers.NewSharedInformerFactoryWithOptions(spokeKubeClient, 24*time.Hour)

	v := &sarCacheValidator{
		kubeClient:     spokeKubeClient,
		validator:      validator,
		executorCaches: executorCaches,
		spokeInformer:  spokeKubeInformerFactory,
	}

	v.cacheController = NewExecutorCacheController(ctx, recorder,
		manifestWorkLister,
		v.spokeInformer.Rbac().V1().ClusterRoleBindings(),
		v.spokeInformer.Rbac().V1().RoleBindings(),
		v.spokeInformer.Rbac().V1().ClusterRoles(),
		v.spokeInformer.Rbac().V1().Roles(),
		restMapper,
		executorCaches,
		v.validator.CheckSubjectAccessReviews,
	)

	return v
}

func (v *sarCacheValidator) Start(ctx context.Context) {
	v.spokeInformer.Start(ctx.Done())
	v.cacheController.Run(ctx, 1)
}

type sarCacheValidator struct {
	kubeClient      kubernetes.Interface
	executorCaches  *store.ExecutorCaches
	validator       *basic.SarValidator
	spokeInformer   informers.SharedInformerFactory
	cacheController factory.Controller
}

func (v *sarCacheValidator) Validate(ctx context.Context, executor *workapiv1.ManifestWorkExecutor,
	gvr schema.GroupVersionResource, namespace, name string,
	ownedByTheWork bool, obj *unstructured.Unstructured) error {
	if executor == nil {
		return nil
	}

	if err := v.validator.ExecutorBasicCheck(executor); err != nil {
		return err
	}

	sa := executor.Subject.ServiceAccount
	executorKey := store.ExecutorKey(sa.Namespace, sa.Name)
	dimension := store.Dimension{
		Namespace:     namespace,
		Name:          name,
		Resource:      gvr.Resource,
		Group:         gvr.Group,
		Version:       gvr.Version,
		ExecuteAction: store.GetExecuteAction(ownedByTheWork),
	}

	if allow, ok := v.executorCaches.Get(executorKey, dimension); !ok {
		err := v.validator.CheckSubjectAccessReviews(ctx, sa, gvr, namespace, name, ownedByTheWork)
		updateSARCheckResultToCache(v.executorCaches, executorKey, dimension, err)
		if err != nil {
			return err
		}
	} else {
		klog.V(4).Infof("Get auth from cache executor %s, dimension: %+v allow: %v", executorKey, dimension, allow)
		if !allow {
			return &basic.NotAllowedError{
				Err: fmt.Errorf("not allowed to apply the resource %s %s, %s %s",
					gvr.Group, gvr.Resource, namespace, name),
				RequeueTime: 60 * time.Second,
			}
		}
	}

	return v.validator.CheckEscalation(ctx, sa, gvr, namespace, name, obj)
}

// updateSARCheckResultToCache updates the subjectAccessReview checking result to the executor cache
func updateSARCheckResultToCache(executorCaches *store.ExecutorCaches, executorKey string,
	dimension store.Dimension, result error) {
	if result == nil {
		executorCaches.Add(executorKey, dimension, true)
	}

	var authError = &basic.NotAllowedError{}
	if errors.As(result, &authError) {
		executorCaches.Add(executorKey, dimension, false)
	}
}
