package spoke

import (
	"context"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic"

	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/open-cluster-management/work/pkg/spoke"
	"github.com/open-cluster-management/work/pkg/spoke/resource"
	"github.com/open-cluster-management/work/test/integration/util"
)

func startWorkAgent(ctx context.Context, o *spoke.WorkloadAgentOptions) {
	err := o.RunWorkloadAgent(ctx, &controllercmd.ControllerContext{
		KubeConfig:    spokeRestConfig,
		EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

var _ = ginkgo.Describe("ManifestWork", func() {
	var o *spoke.WorkloadAgentOptions
	var cancel context.CancelFunc

	var work *workapiv1.ManifestWork
	var manifests []workapiv1.Manifest

	var err error

	ginkgo.BeforeEach(func() {
		o = spoke.NewWorkloadAgentOptions()
		o.HubKubeconfigFile = hubKubeconfigFileName
		o.SpokeClusterName = utilrand.String(5)

		ns := &corev1.Namespace{}
		ns.Name = o.SpokeClusterName
		_, err := spokeKubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		resource.MapperRefreshInterval = 2 * time.Second

		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go startWorkAgent(ctx, o)

		// reset manifests
		manifests = nil
	})

	ginkgo.JustBeforeEach(func() {
		work = util.NewManifestWork(o.SpokeClusterName, "", manifests)
		work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Create(context.Background(), work, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		err := spokeKubeClient.CoreV1().Namespaces().Delete(context.Background(), o.SpokeClusterName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.Context("With a single manifest", func() {
		ginkgo.BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm1", map[string]string{"a": "b"})),
			}
		})

		ginkgo.It("should create work and then apply it successfully", func() {
			util.AssertExistenceOfConfigMaps(manifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should update work and then apply it successfully", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			newManifests := []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm2", map[string]string{"x": "y"})),
			}
			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			work.Spec.Workload.Manifests = newManifests

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertExistenceOfConfigMaps(newManifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
			// TODO: check if resources created by old manifests are deleted
		})

		ginkgo.It("should delete work successfully", func() {
			util.AssertFinalizerAdded(work.Namespace, work.Name, hubWorkClient, eventuallyTimeout, eventuallyInterval)

			err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkDeleted(work.Namespace, work.Name, manifests, hubWorkClient, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})
	})

	ginkgo.Context("With multiple manifests", func() {
		ginkgo.BeforeEach(func() {
			manifests = []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap("non-existent-namespace", "cm1", map[string]string{"a": "b"})),
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm2", map[string]string{"c": "d"})),
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm3", map[string]string{"e": "f"})),
			}
		})

		ginkgo.It("should create work and then apply it successfully", func() {
			util.AssertExistenceOfConfigMaps(manifests[1:], spokeKubeClient, eventuallyTimeout, eventuallyInterval)

			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should update work and then apply it successfully", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionFalse,
				[]metav1.ConditionStatus{metav1.ConditionFalse, metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			newManifests := []workapiv1.Manifest{
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm1", map[string]string{"a": "b"})),
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm2", map[string]string{"x": "y"})),
				util.ToManifest(util.NewConfigmap(o.SpokeClusterName, "cm3", map[string]string{"e": "f"})),
			}

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			work.Spec.Workload.Manifests = newManifests
			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertExistenceOfConfigMaps(newManifests, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
			// TODO: check if resources created by old manifests are deleted
		})

		ginkgo.It("should delete work successfully", func() {
			util.AssertFinalizerAdded(work.Namespace, work.Name, hubWorkClient, eventuallyTimeout, eventuallyInterval)

			err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Delete(context.Background(), work.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertWorkDeleted(work.Namespace, work.Name, manifests, hubWorkClient, spokeKubeClient, eventuallyTimeout, eventuallyInterval)
		})
	})

	ginkgo.Context("With CRD and CR in manifests", func() {
		var spokeDynamicClient dynamic.Interface
		var gvrs []schema.GroupVersionResource
		var objects []*unstructured.Unstructured

		ginkgo.BeforeEach(func() {
			spokeDynamicClient, err = dynamic.NewForConfig(spokeRestConfig)
			gvrs = nil
			objects = nil

			// crd
			obj, gvr, err := util.GuestbookCrd()
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gvrs = append(gvrs, gvr)
			objects = append(objects, obj)

			// cr
			obj, gvr, err = util.GuestbookCr(o.SpokeClusterName, "guestbook1")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gvrs = append(gvrs, gvr)
			objects = append(objects, obj)

			for _, obj := range objects {
				manifests = append(manifests, util.ToManifest(obj))
			}
		})

		ginkgo.It("should create CRD and CR successfully", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue}, eventuallyTimeout, eventuallyInterval)

			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}

			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
			util.AssertAppliedResources(work.Namespace, work.Name, gvrs, namespaces, names, hubWorkClient, eventuallyTimeout, eventuallyInterval)
		})
	})

	ginkgo.Context("With Service Account, Role, RoleBinding and Deployment in manifests", func() {
		var spokeDynamicClient dynamic.Interface
		var gvrs []schema.GroupVersionResource
		var objects []*unstructured.Unstructured

		ginkgo.BeforeEach(func() {
			spokeDynamicClient, err = dynamic.NewForConfig(spokeRestConfig)
			gvrs = nil
			objects = nil

			u, gvr := util.NewServiceAccount(o.SpokeClusterName, "sa")
			gvrs = append(gvrs, gvr)
			objects = append(objects, u)

			u, gvr = util.NewUnstructuredRole(o.SpokeClusterName, "role1")
			gvrs = append(gvrs, gvr)
			objects = append(objects, u)

			u, gvr = util.NewUnstructuredRoleBinding(o.SpokeClusterName, "rolebinding1", "sa", "role1")
			gvrs = append(gvrs, gvr)
			objects = append(objects, u)

			u, gvr, err = util.NewDeployment(o.SpokeClusterName, "deploy1", "sa")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gvrs = append(gvrs, gvr)
			objects = append(objects, u)

			for _, obj := range objects {
				manifests = append(manifests, util.ToManifest(obj))
			}
		})

		ginkgo.It("should create Service Account, Role, RoleBinding and Deployment successfully", func() {
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)

			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}

			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
			util.AssertAppliedResources(work.Namespace, work.Name, gvrs, namespaces, names, hubWorkClient, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should update Service Account and Deployment successfully", func() {
			ginkgo.By("check condition status in work status")
			util.AssertWorkCondition(work.Namespace, work.Name, hubWorkClient, string(workapiv1.WorkApplied), metav1.ConditionTrue,
				[]metav1.ConditionStatus{metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue, metav1.ConditionTrue},
				eventuallyTimeout, eventuallyInterval)

			ginkgo.By("check existence of all maintained resources")
			var namespaces, names []string
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}
			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)

			ginkgo.By("check if applied resources in status are updated")
			util.AssertAppliedResources(work.Namespace, work.Name, gvrs, namespaces, names, hubWorkClient, eventuallyTimeout, eventuallyInterval)

			// update manifests in work: 1) swap service account and deployment; 2) rename service account; 3) update deployment
			ginkgo.By("update manifests in work")
			oldServiceAccount := objects[0]
			gvrs[0], gvrs[3] = gvrs[3], gvrs[0]
			u, _ := util.NewServiceAccount(o.SpokeClusterName, "admin")
			objects[3] = u
			u, _, err = util.NewDeployment(o.SpokeClusterName, "deploy1", "admin")
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			objects[0] = u

			newManifests := []workapiv1.Manifest{}
			for _, obj := range objects {
				newManifests = append(newManifests, util.ToManifest(obj))
			}

			// slow down to make the difference between LastTransitionTime and updateTime large enough for measurement
			time.Sleep(1 * time.Second)
			updateTime := metav1.Now()
			time.Sleep(1 * time.Second)

			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			work.Spec.Workload.Manifests = newManifests
			work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Update(context.Background(), work, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("check existence of all maintained resources")
			namespaces = nil
			names = nil
			for _, obj := range objects {
				namespaces = append(namespaces, obj.GetNamespace())
				names = append(names, obj.GetName())
			}
			util.AssertExistenceOfResources(gvrs, namespaces, names, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)

			ginkgo.By("check if deployment is updated")
			gomega.Eventually(func() bool {
				u, err := util.GetResource(o.SpokeClusterName, objects[0].GetName(), gvrs[0], spokeDynamicClient)
				if err != nil {
					return false
				}

				sa, _, _ := unstructured.NestedString(u.Object, "spec", "template", "spec", "serviceAccountName")
				if "admin" != sa {
					return false
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			ginkgo.By("check if LastTransitionTime is updated")
			gomega.Eventually(func() bool {
				work, err = hubWorkClient.WorkV1().ManifestWorks(o.SpokeClusterName).Get(context.Background(), work.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}
				if len(work.Status.ResourceStatus.Manifests) != len(objects) {
					return false
				}

				for i := range work.Status.ResourceStatus.Manifests {
					if len(work.Status.ResourceStatus.Manifests[i].Conditions) != 1 {
						return false
					}
				}

				// the LastTransitionTime of deployment should not change because of resouce update
				if updateTime.Before(&work.Status.ResourceStatus.Manifests[0].Conditions[0].LastTransitionTime) {
					return false
				}

				// the LastTransitionTime of service account changes because of resource re-creation
				if work.Status.ResourceStatus.Manifests[3].Conditions[0].LastTransitionTime.Before(&updateTime) {
					return false
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			ginkgo.By("check if applied resources in status are updated")
			util.AssertAppliedResources(work.Namespace, work.Name, gvrs, namespaces, names, hubWorkClient, eventuallyTimeout, eventuallyInterval)

			ginkgo.By("check if resources which are no longer maintained have been deleted")
			util.AssertNonexistenceOfResources([]schema.GroupVersionResource{gvrs[3]}, []string{oldServiceAccount.GetNamespace()}, []string{oldServiceAccount.GetName()}, spokeDynamicClient, eventuallyTimeout, eventuallyInterval)
		})
	})
})
