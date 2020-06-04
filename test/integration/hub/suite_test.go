package hub

import (
	"path/filepath"
	"testing"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	"github.com/go-logr/logr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	workclientset "github.com/open-cluster-management/api/client/work/clientset/versioned"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

var restConfig *rest.Config
var testEnv *envtest.Environment
var kubeClient kubernetes.Interface
var workClient workclientset.Interface
var log logr.Logger

func TestIntegration(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Integration Suite")
}

var _ = ginkgo.BeforeSuite(func(done ginkgo.Done) {
	log = zap.LoggerTo(ginkgo.GinkgoWriter, true)
	logf.SetLogger(log)
	ginkgo.By("bootstrapping test environment")

	// start a kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join(".", "vendor", "github.com", "open-cluster-management", "api", "work", "v1"),
		},
	}

	cfg, err := testEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	/*
		// create kubeconfig file for hub in a tmp dir
		tempDir, err = ioutil.TempDir("", "test")
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(tempDir).ToNot(gomega.BeEmpty())
		hubKubeconfigFileName = path.Join(tempDir, "kubeconfig")
		err = util.CreateKubeconfigFile(cfg, hubKubeconfigFileName)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		err = apiextensionsv1beta1.AddToScheme(scheme.Scheme)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	*/

	err = workapiv1.AddToScheme(scheme.Scheme)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	restConfig = cfg
	kubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	workClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	close(done)
}, 60)

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})
