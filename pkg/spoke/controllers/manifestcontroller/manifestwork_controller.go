package manifestcontroller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"

	workv1client "github.com/open-cluster-management/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "github.com/open-cluster-management/api/client/work/informers/externalversions/work/v1"
	worklister "github.com/open-cluster-management/api/client/work/listers/work/v1"
	workapiv1 "github.com/open-cluster-management/api/work/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"

	"github.com/open-cluster-management/work/pkg/helper"
	"github.com/open-cluster-management/work/pkg/spoke/controllers"
	"github.com/open-cluster-management/work/pkg/spoke/resource"
)

const specHashAnnotation = "open-cluster-management.io/spec-hash"

// ManifestWorkController is to reconcile the workload resources
// fetched from hub cluster on spoke cluster.
type ManifestWorkController struct {
	manifestWorkClient        workv1client.ManifestWorkInterface
	manifestWorkLister        worklister.ManifestWorkNamespaceLister
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface
	appliedManifestWorkLister worklister.AppliedManifestWorkLister
	spokeDynamicClient        dynamic.Interface
	spokeKubeclient           kubernetes.Interface
	spokeAPIExtensionClient   apiextensionsclient.Interface
	hubHash                   string
	// restMapper is a cached resource mapping obtained fron discovery client
	restMapper *resource.Mapper
}

// NewManifestWorkController returns a ManifestWorkController
func NewManifestWorkController(
	ctx context.Context,
	recorder events.Recorder,
	spokeDynamicClient dynamic.Interface,
	spokeKubeClient kubernetes.Interface,
	spokeAPIExtensionClient apiextensionsclient.Interface,
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	appliedManifestWorkInformer workinformer.AppliedManifestWorkInformer,
	hubHash string,
	restMapper *resource.Mapper) factory.Controller {

	controller := &ManifestWorkController{
		manifestWorkClient:        manifestWorkClient,
		manifestWorkLister:        manifestWorkLister,
		appliedManifestWorkClient: appliedManifestWorkClient,
		appliedManifestWorkLister: appliedManifestWorkInformer.Lister(),
		spokeDynamicClient:        spokeDynamicClient,
		spokeKubeclient:           spokeKubeClient,
		spokeAPIExtensionClient:   spokeAPIExtensionClient,
		hubHash:                   hubHash,
		restMapper:                restMapper,
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, manifestWorkInformer.Informer()).
		WithInformersQueueKeyFunc(helper.AppliedManifestworkQueueKeyFunc(hubHash), appliedManifestWorkInformer.Informer()).
		WithSync(controller.sync).ResyncEvery(5*time.Minute).ToController("ManifestWorkAgent", recorder)
}

// sync is the main reconcile loop for manifest work. It is triggered in two scenarios
// 1. ManifestWork API changes
// 2. Resources defined in manifest changed on spoke
func (m *ManifestWorkController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	manifestWorkName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ManifestWork %q", manifestWorkName)

	manifestWork, err := m.manifestWorkLister.Get(manifestWorkName)
	if errors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	manifestWork = manifestWork.DeepCopy()

	// no work to do if we're deleted
	if !manifestWork.DeletionTimestamp.IsZero() {
		return nil
	}

	// don't do work if the finalizer is not present
	// it ensures all maintained resources will be cleaned once manifestwork is deleted
	found := false
	for i := range manifestWork.Finalizers {
		if manifestWork.Finalizers[i] == controllers.ManifestWorkFinalizer {
			found = true
			break
		}
	}
	if !found {
		return nil
	}
	// Apply appliedManifestWork
	appliedManifestWorkName := fmt.Sprintf("%s-%s", m.hubHash, manifestWork.Name)
	appliedManifestWork, err := m.appliedManifestWorkLister.Get(appliedManifestWorkName)
	switch {
	case errors.IsNotFound(err):
		appliedManifestWork = &workapiv1.AppliedManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:       appliedManifestWorkName,
				Finalizers: []string{controllers.AppliedManifestWorkFinalizer},
			},
			Spec: workapiv1.AppliedManifestWorkSpec{
				HubHash:          m.hubHash,
				ManifestWorkName: manifestWorkName,
			},
		}
		appliedManifestWork, err = m.appliedManifestWorkClient.Create(ctx, appliedManifestWork, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	case err != nil:
		return err
	}
	owner := metav1.NewControllerRef(appliedManifestWork, workapiv1.GroupVersion.WithKind("AppliedManifestWork"))

	errs := []error{}
	// Apply resources on spoke cluster.
	resourceResults := m.applyManifest(
		manifestWork.Spec.Workload.Manifests,
		manifestWork.Status.ResourceStatus.Manifests, controllerContext.Recorder(), *owner)
	newManifestConditions := []workapiv1.ManifestCondition{}
	for index, result := range resourceResults {
		if result.Error != nil {
			errs = append(errs, result.Error)
		}

		resourceMeta, generation, err := buildManifestResourceMeta(index, result.Result, manifestWork.Spec.Workload.Manifests[index], m.restMapper)
		if err != nil {
			errs = append(errs, err)
		}

		manifestCondition := workapiv1.ManifestCondition{
			ResourceMeta: resourceMeta,
			Conditions:   []metav1.Condition{},
		}

		// Add applied status condition
		manifestCondition.Conditions = append(manifestCondition.Conditions, buildAppliedStatusCondition(result.Error, generation))

		newManifestConditions = append(newManifestConditions, manifestCondition)
	}

	// Update work status
	_, _, err = helper.UpdateManifestWorkStatus(
		ctx, m.manifestWorkClient, manifestWork.Name, m.generateUpdateStatusFunc(newManifestConditions))
	if err != nil {
		errs = append(errs, fmt.Errorf("Failed to update work status with err %w", err))
	}
	if len(errs) > 0 {
		err = utilerrors.NewAggregate(errs)
		klog.Errorf("Reconcile work %s fails with err: %v", manifestWorkName, err)
	}
	return err
}

// Use typed apply func at first, if the type is not recognized, fallback to use unstructured apply
func (m *ManifestWorkController) applyManifest(
	manifests []workapiv1.Manifest, resourcesStatus []workapiv1.ManifestCondition, recorder events.Recorder, owner metav1.OwnerReference) []resourceapply.ApplyResult {
	clientHolder := resourceapply.NewClientHolder().
		WithAPIExtensionsClient(m.spokeAPIExtensionClient).
		WithKubernetes(m.spokeKubeclient)

	// Using index as the file name and apply manifests
	indexstrings := []string{}
	for i := 0; i < len(manifests); i++ {
		indexstrings = append(indexstrings, strconv.Itoa(i))
	}
	results := resourceapply.ApplyDirectly(clientHolder, recorder, func(name string) ([]byte, error) {
		index, _ := strconv.ParseInt(name, 10, 32)
		unstructuredObj := &unstructured.Unstructured{}
		err := unstructuredObj.UnmarshalJSON(manifests[index].Raw)
		if err != nil {
			return nil, err
		}
		unstructuredObj.SetOwnerReferences([]metav1.OwnerReference{owner})
		return unstructuredObj.MarshalJSON()
	}, indexstrings...)

	// Try apply with dynamic client if the manifest cannot be decoded by scheme or typed client is not found
	// TODO we should check the certain error.
	for index, result := range results {
		// Use dynamic client when scheme cannot decode manifest or typed client cannot handle the object
		var status *workapiv1.ManifestCondition
		if len(resourcesStatus) > index {
			status = &resourcesStatus[index]
		}
		if isDecodeError(result.Error) || isUnhandledError(result.Error) {
			results[index].Result, results[index].Changed, results[index].Error = m.applyUnstructrued(manifests[index].Raw, status, owner, recorder)
		}
	}
	return results
}

func (m *ManifestWorkController) decodeUnstructured(data []byte) (schema.GroupVersionResource, *unstructured.Unstructured, error) {
	unstructuredObj := &unstructured.Unstructured{}
	err := unstructuredObj.UnmarshalJSON(data)
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("Failed to decode object: %w", err)
	}
	mapping, err := m.restMapper.MappingForGVK(unstructuredObj.GroupVersionKind())
	if err != nil {
		return schema.GroupVersionResource{}, nil, fmt.Errorf("Failed to find gvr from restmapping: %w", err)
	}

	return mapping.Resource, unstructuredObj, nil
}

// Update resource when
// 1. metadat is changed
// 2. spec hash is changed
// 3. generation is changed
func (m *ManifestWorkController) applyUnstructrued(
	data []byte, resourceMeta *workapiv1.ManifestCondition, owner metav1.OwnerReference, recorder events.Recorder) (*unstructured.Unstructured, bool, error) {
	gvr, required, err := m.decodeUnstructured(data)
	if err != nil {
		return nil, false, err
	}

	required.SetOwnerReferences([]metav1.OwnerReference{owner})
	setSpecHashAnnotation(required)
	existing, err := m.spokeDynamicClient.
		Resource(gvr).
		Namespace(required.GetNamespace()).
		Get(context.TODO(), required.GetName(), metav1.GetOptions{})
	if errors.IsNotFound(err) {
		actual, err := m.spokeDynamicClient.Resource(gvr).Namespace(required.GetNamespace()).Create(
			context.TODO(), required, metav1.CreateOptions{})
		recorder.Eventf(fmt.Sprintf(
			"%s Created", required.GetKind()), "Created %s/%s because it was missing", required.GetNamespace(), required.GetName())
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	if isManifestModified(resourceMeta, gvr, existing, required) {
		required.SetResourceVersion(existing.GetResourceVersion())
		actual, err := m.spokeDynamicClient.Resource(gvr).Namespace(required.GetNamespace()).Update(
			context.TODO(), required, metav1.UpdateOptions{})
		recorder.Eventf(fmt.Sprintf(
			"%s Updated", required.GetKind()), "Updated %s/%s", required.GetNamespace(), required.GetName())
		return actual, true, err
	}

	return existing, false, nil
}

// generateUpdateStatusFunc returns a function which aggregates manifest conditions and generates work conditions.
// Rules to generate work status conditions from manifest conditions
// #1: Applied - work status condition (with type Applied) is applied if all manifest conditions (with type Applied) are applied
// TODO: add rules for other condition types, like Progressing, Available, Degraded
func (m *ManifestWorkController) generateUpdateStatusFunc(newManifestConditions []workapiv1.ManifestCondition) helper.UpdateManifestWorkStatusFunc {
	return func(oldStatus *workapiv1.ManifestWorkStatus) error {
		// merge the new manifest conditions with the existing manifest conditions
		oldStatus.ResourceStatus.Manifests = helper.MergeManifestConditions(oldStatus.ResourceStatus.Manifests, newManifestConditions)

		// aggregate manifest condition to generate work condition
		newConditions := []metav1.Condition{}

		// handle condition type Applied
		if inCondition, exists := allInCondition(string(workapiv1.ManifestApplied), newManifestConditions); exists {
			appliedCondition := metav1.Condition{
				Type: workapiv1.WorkApplied,
			}
			if inCondition {
				appliedCondition.Status = metav1.ConditionTrue
				appliedCondition.Reason = "AppliedManifestWorkComplete"
				appliedCondition.Message = "Apply manifest work complete"
			} else {
				appliedCondition.Status = metav1.ConditionFalse
				appliedCondition.Reason = "AppliedManifestWorkFailed"
				appliedCondition.Message = "Failed to apply manifest work"
			}
			newConditions = append(newConditions, appliedCondition)
		}

		oldStatus.Conditions = helper.MergeStatusConditions(oldStatus.Conditions, newConditions)
		return nil
	}
}

// We need to check if the resource meta equals at first since the condition could map to another resource.
// return true if the resourcemeta is not matched
// Otherwise, return true when label/annotation is changed or generation is changed
func isManifestModified(manifestStatus *workapiv1.ManifestCondition, gvr schema.GroupVersionResource, existing, required *unstructured.Unstructured) bool {
	// Return true if manifeststatus cannot be found
	if manifestStatus == nil {
		return true
	}

	requiredResourceMeta := workapiv1.ManifestResourceMeta{
		Ordinal:   manifestStatus.ResourceMeta.Ordinal,
		Group:     gvr.Group,
		Resource:  gvr.Resource,
		Kind:      required.GetKind(),
		Version:   gvr.Version,
		Namespace: required.GetNamespace(),
		Name:      required.GetName(),
	}

	//  return true if resourcemeta is not matched.
	if requiredResourceMeta != manifestStatus.ResourceMeta {
		return true
	}

	condition := meta.FindStatusCondition(manifestStatus.Conditions, string(workapiv1.ManifestApplied))
	// return true if applied condition is not found
	if condition == nil {
		return true
	}

	if !isSameUnstructuredMeta(required, existing) {
		return true
	}

	if existing.GetGeneration() != condition.ObservedGeneration {
		return true
	}

	return false
}

// isDecodeError is to check if the error returned from resourceapply is due to that the object cannnot
// be decoded or no typed client can handle the object.
func isDecodeError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "cannot decode")
}

// isUnhandledError is to check if the error returned from resourceapply is due to that no typed
// client can handle the object
func isUnhandledError(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "unhandled type")
}

// isSameUnstructuredMeta compares the metadata of two unstructured object.
func isSameUnstructuredMeta(obj1, obj2 *unstructured.Unstructured) bool {
	// Comapre gvk, name, namespace at first
	if obj1.GroupVersionKind() != obj2.GroupVersionKind() {
		return false
	}
	if obj1.GetName() != obj2.GetName() {
		return false
	}
	if obj1.GetNamespace() != obj2.GetNamespace() {
		return false
	}

	// Compare label and annotations
	if !equality.Semantic.DeepEqual(obj1.GetLabels(), obj2.GetLabels()) {
		return false
	}
	if !equality.Semantic.DeepEqual(obj1.GetAnnotations(), obj2.GetAnnotations()) {
		return false
	}

	return true
}

// setSpecHashAnnotation computes the hash of the provided spec and sets an annotation of the
// hash on the provided unstructured objectt. This method is used internally by Apply<type> methods.
func setSpecHashAnnotation(obj *unstructured.Unstructured) error {
	data := obj.DeepCopy().Object
	// do not hash metadata and status section
	delete(data, "metadata")
	delete(data, "status")

	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	specHash := fmt.Sprintf("%x", sha256.Sum256(jsonBytes))
	annotation := obj.GetAnnotations()
	if annotation == nil {
		annotation = map[string]string{}
	}
	annotation[specHashAnnotation] = specHash
	obj.SetAnnotations(annotation)
	return nil
}

// allInCondition checks status of conditions with a particular type in ManifestCondition array.
// Return true only if conditions with the condition type exist and they are all in condition.
func allInCondition(conditionType string, manifests []workapiv1.ManifestCondition) (inCondition bool, exists bool) {
	for _, manifest := range manifests {
		for _, condition := range manifest.Conditions {
			if condition.Type == conditionType {
				exists = true
			}

			if condition.Type == conditionType && condition.Status == metav1.ConditionFalse {
				return false, true
			}
		}
	}

	return exists, exists
}

func buildAppliedStatusCondition(err error, generation int64) metav1.Condition {
	if err != nil {
		return metav1.Condition{
			Type:               string(workapiv1.ManifestApplied),
			Status:             metav1.ConditionFalse,
			ObservedGeneration: generation,
			Reason:             "AppliedManifestFailed",
			Message:            fmt.Sprintf("Failed to apply manifest: %v", err),
		}
	}

	return metav1.Condition{
		Type:               string(workapiv1.ManifestApplied),
		Status:             metav1.ConditionTrue,
		ObservedGeneration: generation,
		Reason:             "AppliedManifestComplete",
		Message:            "Apply manifest complete",
	}
}

// buildManifestResourceMeta returns resource meta for manifest. It tries to get the resource
// meta from the result object in ApplyResult struct. If the resource meta is incompleted, fall
// back to manifest template for the meta info.
func buildManifestResourceMeta(index int, object runtime.Object, manifest workapiv1.Manifest, restMapper *resource.Mapper) (resourceMeta workapiv1.ManifestResourceMeta, generation int64, err error) {
	errs := []error{}

	resourceMeta, generation, err = buildResourceMeta(index, object, restMapper)
	if err != nil {
		errs = append(errs, err)
	} else if len(resourceMeta.Kind) > 0 && len(resourceMeta.Version) > 0 && len(resourceMeta.Name) > 0 {
		return resourceMeta, generation, nil
	}

	// try to get resource meta from manifest if the one got from apply result is incompleted
	switch {
	case manifest.Object != nil:
		object = manifest.Object
	default:
		unstructuredObj := &unstructured.Unstructured{}
		if err = unstructuredObj.UnmarshalJSON(manifest.Raw); err != nil {
			errs = append(errs, err)
			return resourceMeta, generation, utilerrors.NewAggregate(errs)
		}
		object = unstructuredObj
	}
	resourceMeta, generation, err = buildResourceMeta(index, object, restMapper)
	if err == nil {
		return resourceMeta, generation, nil
	}

	return resourceMeta, generation, utilerrors.NewAggregate(errs)
}

func buildResourceMeta(index int, object runtime.Object, restMapper *resource.Mapper) (resourceMeta workapiv1.ManifestResourceMeta, generation int64, err error) {

	resourceMeta = workapiv1.ManifestResourceMeta{
		Ordinal: int32(index),
	}

	if object == nil || reflect.ValueOf(object).IsNil() {
		return resourceMeta, generation, err
	}

	// set gvk
	gvk, err := helper.GuessObjectGroupVersionKind(object)
	if err != nil {
		return resourceMeta, generation, err
	}
	resourceMeta.Group = gvk.Group
	resourceMeta.Version = gvk.Version
	resourceMeta.Kind = gvk.Kind

	// set namespace/name
	if accessor, e := meta.Accessor(object); e != nil {
		err = fmt.Errorf("cannot access metadata of %v: %w", object, e)
	} else {
		resourceMeta.Namespace = accessor.GetNamespace()
		resourceMeta.Name = accessor.GetName()
		generation = accessor.GetGeneration()
	}

	// set resource
	if restMapper == nil {
		return resourceMeta, generation, err
	}
	if mapping, e := restMapper.MappingForGVK(*gvk); e == nil {
		resourceMeta.Resource = mapping.Resource.Resource
	}

	return resourceMeta, generation, err
}
