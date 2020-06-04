package hub

import (
	"context"
	"time"

	workclientset "github.com/open-cluster-management/api/client/work/clientset/versioned"
	workinformers "github.com/open-cluster-management/api/client/work/informers/externalversions"
	"github.com/open-cluster-management/work/pkg/hub/controllers/finalizercontroller"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
)

// RunControllerManager starts the controllers on hub to manage manifest works.
func RunControllerManager(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	// setup kube informers
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	kubeInformers := kubeinformers.NewSharedInformerFactory(kubeClient, 5*time.Minute)

	// setup work informers
	workClient, err := workclientset.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}
	workInformers := workinformers.NewSharedInformerFactory(workClient, 5*time.Minute)
	workLister := workInformers.Work().V1().ManifestWorks().Lister()

	// create controllers
	finalizeController := finalizercontroller.NewFinalizeController(
		controllerContext.EventRecorder,
		kubeInformers.Rbac().V1().Roles(),
		kubeInformers.Rbac().V1().RoleBindings(),
		kubeInformers.Core().V1().Namespaces().Lister(),
		workClient.WorkV1(),
		workLister,
		kubeClient.RbacV1())

	// start informers and controllers
	go kubeInformers.Start(ctx.Done())
	go workInformers.Start(ctx.Done())

	go finalizeController.Run(ctx, 1)

	<-ctx.Done()
	return nil
}
