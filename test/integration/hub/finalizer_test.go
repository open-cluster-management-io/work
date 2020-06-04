package hub

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/open-cluster-management/work/pkg/hub"
	"github.com/open-cluster-management/work/pkg/hub/controllers/finalizercontroller"
	"github.com/open-cluster-management/work/test/integration/util"
)

const (
	manifestWorkFinalizer = "cluster.open-cluster-management.io/manifest-work-cleanup"
)

func startControllerManager(ctx context.Context) {
	err := hub.RunControllerManager(ctx, &controllercmd.ControllerContext{
		KubeConfig:    restConfig,
		EventRecorder: events.NewInMemoryRecorder(""),
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func startFakeWorkAgent(ctx context.Context, clusterName string) {
	log.Info("Start fake WorkAgent...")
Loop:
	for {
		select {
		case <-ctx.Done():
			break Loop
		default:
			works, err := workClient.WorkV1().ManifestWorks(clusterName).List(context.Background(), metav1.ListOptions{})
			if err != nil {
				log.Error(err, "Unable to list works")
			} else {
				for _, work := range works.Items {
					if work.DeletionTimestamp == nil || work.DeletionTimestamp.IsZero() {
						continue
					}

					if len(work.Finalizers) == 0 {
						continue
					}

					work.Finalizers = nil
					_, err := workClient.WorkV1().ManifestWorks(clusterName).Update(context.Background(), &work, metav1.UpdateOptions{})
					if err != nil {
						log.Error(err, "Unable to update work", "namespace", work.Namespace, "name", work.Name)
					} else {
						log.Info("Remove finalizer from work", "namespace", work.Namespace, "name", work.Name)
					}
				}
			}
			time.Sleep(1 * time.Second)
		}
	}

	log.Info("Fake WorkAgent exited")
}

var _ = ginkgo.Describe("Finalizer controllers", func() {
	var ctx context.Context
	var cancel context.CancelFunc
	var works []*workapiv1.ManifestWork
	var clusterName string
	var err error

	ginkgo.BeforeEach(func() {
		finalizercontroller.RequeueDelay = 1 * time.Second

		// create cluster namespace
		ns := &corev1.Namespace{}
		ns.GenerateName = "cluster-"
		ns, err = kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		clusterName = ns.Name

		// create works
		for i := 0; i < 5; i++ {
			work := util.NewManifestWork(clusterName, fmt.Sprintf("work%d", i+1), nil)
			work.Finalizers = []string{manifestWorkFinalizer}
			work, err = workClient.WorkV1().ManifestWorks(clusterName).Create(context.Background(), work, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			works = append(works, work)
		}

		ctx, cancel = context.WithCancel(context.Background())
		// start hub controllers
		go startControllerManager(ctx)

		// create role/rolebinding in cluster namespace
		role := util.NewRole(clusterName, fmt.Sprintf("%s:spoke-work", clusterName), []string{manifestWorkFinalizer})
		_, err = kubeClient.RbacV1().Roles(clusterName).Create(context.Background(), role, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		rolebinding := util.NewRoleBinding(clusterName, fmt.Sprintf("%s:spoke-work", clusterName), []string{manifestWorkFinalizer})
		_, err = kubeClient.RbacV1().RoleBindings(clusterName).Create(context.Background(), rolebinding, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		ns, err := kubeClient.CoreV1().Namespaces().Get(context.Background(), clusterName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			return
		}
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		if ns.DeletionTimestamp == nil || ns.DeletionTimestamp.IsZero() {
			err := kubeClient.CoreV1().Namespaces().Delete(context.Background(), clusterName, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}
	})

	ginkgo.Context("Within terminating namespace", func() {
		ginkgo.It("should delete both manifest works and role/rolebinding", func() {
			// start a fake work agent to remove finalizer from works
			go startFakeWorkAgent(ctx, clusterName)

			deleteNamespace(clusterName)
			util.AssertNamespaceDeleted(clusterName, kubeClient, workClient, eventuallyTimeout, eventuallyInterval)
		})
	})

	ginkgo.Context("Within non-terminating namespace", func() {
		ginkgo.It("should delete only role/rolebinding and keep existing manifest works", func() {
			// delete role/rolebinding
			err = kubeClient.RbacV1().Roles(clusterName).Delete(context.Background(), fmt.Sprintf("%s:spoke-work", clusterName), metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			err = kubeClient.RbacV1().RoleBindings(clusterName).Delete(context.Background(), fmt.Sprintf("%s:spoke-work", clusterName), metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// check if role/rolebinding are deleted
			gomega.Eventually(func() bool {
				_, err = kubeClient.RbacV1().Roles(clusterName).Get(context.Background(), fmt.Sprintf("%s:spoke-work", clusterName), metav1.GetOptions{})
				if !errors.IsNotFound(err) {
					return false
				}

				_, err = kubeClient.RbacV1().RoleBindings(clusterName).Get(context.Background(), fmt.Sprintf("%s:spoke-work", clusterName), metav1.GetOptions{})
				if !errors.IsNotFound(err) {
					return false
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// check if works still exist
			works, err := workClient.WorkV1().ManifestWorks(clusterName).List(context.Background(), metav1.ListOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(works.Items).ToNot(gomega.BeEmpty())
		})
	})
})

func deleteNamespace(namespace string) {
	// delete namespace
	err := kubeClient.CoreV1().Namespaces().Delete(context.Background(), namespace, metav1.DeleteOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// delete works
	works, err := workClient.WorkV1().ManifestWorks(namespace).List(context.Background(), metav1.ListOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	for _, work := range works.Items {
		err = workClient.WorkV1().ManifestWorks(namespace).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	// delete role/rolebinding
	err = kubeClient.RbacV1().Roles(namespace).Delete(context.Background(), fmt.Sprintf("%s:spoke-work", namespace), metav1.DeleteOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	err = kubeClient.RbacV1().RoleBindings(namespace).Delete(context.Background(), fmt.Sprintf("%s:spoke-work", namespace), metav1.DeleteOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
}
