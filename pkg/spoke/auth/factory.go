package auth

import (
	"context"

	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/work/pkg/spoke/auth/basic"
	"open-cluster-management.io/work/pkg/spoke/auth/cache"
)

// ExecutorValidator validates whether the executor has permission to perform the requests
// to the local managed cluster
type ExecutorValidator interface {
	// Validate whether the work executor subject has permission to perform action on the specific manifest,
	// if there is no permission will return a kubernetes forbidden error.
	Validate(ctx context.Context, executor *workapiv1.ManifestWorkExecutor, gvr schema.GroupVersionResource,
		namespace, name string, ownedByTheWork bool, obj *unstructured.Unstructured) error
}

type validatorFactory struct {
	config             *rest.Config
	kubeClient         kubernetes.Interface
	manifestWorkLister worklister.ManifestWorkNamespaceLister
	recorder           events.Recorder
	restMapper         meta.RESTMapper
}

func NewFactory(
	config *rest.Config,
	kubeClient kubernetes.Interface,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	recorder events.Recorder,
	restMapper meta.RESTMapper) *validatorFactory {
	return &validatorFactory{
		config:             config,
		kubeClient:         kubeClient,
		manifestWorkLister: manifestWorkLister,
		recorder:           recorder,
		restMapper:         restMapper,
	}
}

func (f *validatorFactory) NewExecutorValidator(ctx context.Context, isCacheValidator bool) ExecutorValidator {
	sarValidator := basic.NewSARValidator(f.config, f.kubeClient)
	if !isCacheValidator {
		return sarValidator
	}

	cacheValidator := cache.NewExecutorCacheValidator(
		ctx,
		f.recorder,
		f.kubeClient,
		f.manifestWorkLister,
		f.restMapper,
		sarValidator,
	)
	go cacheValidator.Start(ctx)
	return cacheValidator
}
