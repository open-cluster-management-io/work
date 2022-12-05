package placemanifestworkcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	//"k8s.io/apimachinery/pkg/api/equality"
	//apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	//metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	//"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	//"k8s.io/apimachinery/pkg/types"
	//utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"

	//jsonpatch "github.com/evanphx/json-patch"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	//"github.com/pkg/errors"
	workv1alpha1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1alpha1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1alpha1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1alpha1"
	//workapiv1alpha1 "open-cluster-management.io/api/work/v1alpha1"

	"open-cluster-management.io/work/pkg/helper"
	"open-cluster-management.io/work/pkg/spoke/apply"
	"open-cluster-management.io/work/pkg/spoke/auth"
	"open-cluster-management.io/work/pkg/spoke/auth/basic"
	"open-cluster-management.io/work/pkg/spoke/controllers"
)

var (
	ResyncInterval = 15 * time.Second
)

// ManifestWorkController is to reconcile the workload resources
// fetched from hub cluster on spoke cluster.
type PlaceManifestWorkController struct {
	placeManifestWorkClient workv1alpha1client.PlaceManifestWorkInterface
	placeManifestWorkLister worklister.PlaceManifestWorkNamespaceLister
	restMapper              meta.RESTMapper
	validator               auth.ExecutorValidator
}

type applyResult struct {
	Result runtime.Object
	Error  error
}

// NewManifestWorkController returns a ManifestWorkController
func NewManifestWorkController(
	ctx context.Context,
	recorder events.Recorder,
	placeManifestWorkClient workv1alpha1client.PlaceManifestWorkInterface,
	placeManifestWorkInformer workinformer.PlaceManifestWorkInformer,
	manifestWorkLister worklister.PlaceManifestWorkNamespaceLister,
	restMapper meta.RESTMapper,
	validator auth.ExecutorValidator) factory.Controller {

	controller := &PlaceManifestWorkController{
		placeManifestWorkClient: placeManifestWorkClient,
		placeManifestWorkLister: placeManifestWorkInformer.Lister(),
		restMapper:              restMapper,
		validator:               validator,
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, placeManifestWorkInformer.Informer()).
		WithSync(controller.sync).ResyncEvery(ResyncInterval)
}

// sync is the main reconcile loop for placeManifest work. It is triggered every 15sec
func (m *PlaceManifestWorkController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	placeManifestWorkName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling placeManifestWork %q", placeManifestWorkName)

	return err
}
