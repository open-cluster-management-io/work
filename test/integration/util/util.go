package util

import (
	"fmt"

	"github.com/onsi/ginkgo"

	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	workapiv1 "github.com/open-cluster-management/api/work/v1"
)

func HaveCondition(conditions []workapiv1.StatusCondition, expectedType string, expectedStatus metav1.ConditionStatus) bool {
	found := false
	for _, condition := range conditions {
		if condition.Type != expectedType {
			continue
		}
		found = true

		if condition.Status != expectedStatus {
			return false
		}
		return true
	}

	return found
}

func HaveManifestCondition(conditions []workapiv1.ManifestCondition, expectedType string, expectedStatuses []metav1.ConditionStatus) bool {
	if len(conditions) != len(expectedStatuses) {
		return false
	}

	for index, condition := range conditions {
		expectedStatus := expectedStatuses[index]
		if expectedStatus == "" {
			continue
		}

		if ok := HaveCondition(condition.Conditions, expectedType, expectedStatus); !ok {
			return false
		}
	}

	return true
}

func NewManifestWork(namespace, name string, manifests []workapiv1.Manifest) *workapiv1.ManifestWork {
	work := &workapiv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
		},
		Spec: workapiv1.ManifestWorkSpec{
			Workload: workapiv1.ManifestsTemplate{
				Manifests: manifests,
			},
		},
	}

	if name != "" {
		work.Name = name
	} else {
		work.GenerateName = "work-"
	}

	return work
}

func NewConfigmap(namespace, name string, data map[string]string) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: data,
	}

	return cm
}

func ToManifest(object runtime.Object) workapiv1.Manifest {
	manifest := workapiv1.Manifest{}
	manifest.Object = object
	return manifest
}

func CreateKubeconfigFile(clientConfig *rest.Config, filename string) error {
	// Build kubeconfig.
	kubeconfig := clientcmdapi.Config{
		Clusters: map[string]*clientcmdapi.Cluster{"default-cluster": {
			Server:                   clientConfig.Host,
			InsecureSkipTLSVerify:    clientConfig.Insecure,
			CertificateAuthorityData: clientConfig.CAData,
		}},
		AuthInfos: map[string]*clientcmdapi.AuthInfo{"default-auth": {
			ClientCertificate:     clientConfig.CertFile,
			ClientCertificateData: clientConfig.CertData,
			ClientKey:             clientConfig.KeyFile,
			ClientKeyData:         clientConfig.KeyData,
		}},
		Contexts: map[string]*clientcmdapi.Context{"default-context": {
			Cluster:   "default-cluster",
			AuthInfo:  "default-auth",
			Namespace: "configuration",
		}},
		CurrentContext: "default-context",
	}

	return clientcmd.WriteToFile(kubeconfig, filename)
}

func NewRole(namespace, name string, finalizers []string) *rbacv1.Role {
	role := &rbacv1.Role{
		Rules: []rbacv1.PolicyRule{
			{
				Verbs:     []string{"create", "get", "list", "watch"},
				APIGroups: []string{""},
				Resources: []string{"configmaps"},
			},
		},
	}

	role.Namespace = namespace
	role.Name = name
	role.Finalizers = finalizers

	return role
}

func NewRoleBinding(namespace, name string, finalizers []string) *rbacv1.RoleBinding {
	rolebinding := &rbacv1.RoleBinding{
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     name,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Namespace: namespace,
				Name:      "default",
			},
		},
	}

	rolebinding.Namespace = namespace
	rolebinding.Name = name
	rolebinding.Finalizers = finalizers

	return rolebinding
}

func HasFinalizer(obj runtime.Object, finalizer string) bool {
	accessor, _ := meta.Accessor(obj)
	for _, f := range accessor.GetFinalizers() {
		if f == finalizer {
			return true
		}
	}
	return false
}

func NewIntegrationTestEventRecorder(componet string) events.Recorder {
	return &IntegrationTestEventRecorder{component: componet}
}

type IntegrationTestEventRecorder struct {
	component string
}

func (r *IntegrationTestEventRecorder) ComponentName() string {
	return r.component
}

func (r *IntegrationTestEventRecorder) ForComponent(c string) events.Recorder {
	return &IntegrationTestEventRecorder{component: c}
}

func (r *IntegrationTestEventRecorder) WithComponentSuffix(suffix string) events.Recorder {
	return r.ForComponent(fmt.Sprintf("%s-%s", r.ComponentName(), suffix))
}

func (r *IntegrationTestEventRecorder) Event(reason, message string) {
	fmt.Fprintf(ginkgo.GinkgoWriter, "Event: [%s] %v: %v \n", r.component, reason, message)
}

func (r *IntegrationTestEventRecorder) Eventf(reason, messageFmt string, args ...interface{}) {
	r.Event(reason, fmt.Sprintf(messageFmt, args...))
}

func (r *IntegrationTestEventRecorder) Warning(reason, message string) {
	fmt.Fprintf(ginkgo.GinkgoWriter, "Warning: [%s] %v: %v \n", r.component, reason, message)
}

func (r *IntegrationTestEventRecorder) Warningf(reason, messageFmt string, args ...interface{}) {
	r.Warning(reason, fmt.Sprintf(messageFmt, args...))
}
