package statusfeedback

import (
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/util/jsonpath"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/spoke/statusfeedback/rules"
)

type StatusReader struct {
	wellKnownStatus rules.WellKnownStatusRuleResolver
}

func NewStatusReader() *StatusReader {
	return &StatusReader{
		wellKnownStatus: rules.DefaultWellKnownStatusRule(),
	}
}

func (s *StatusReader) GetValuesByRule(obj *unstructured.Unstructured, rule workapiv1.FeedbackRule) ([]workapiv1.FeedbackValue, error) {
	errs := []error{}
	values := []workapiv1.FeedbackValue{}

	switch rule.Type {
	case workapiv1.WellKnownStatusType:
		gvk := obj.GroupVersionKind()
		paths := s.wellKnownStatus.GetPathsByKind(gvk.GroupKind())
		if len(paths) == 0 {
			break
		}

		for _, path := range paths {
			value, err := getValueByJsonPath(path.Name, path.Path, obj)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if value == nil {
				continue
			}
			values = append(values, *value)
		}
	case workapiv1.JSONPathsType:
		for _, path := range rule.JsonPaths {
			// skip if version is specified and the object version does not match
			if len(path.Version) != 0 && obj.GroupVersionKind().Version != path.Version {
				continue
			}

			value, err := getValueByJsonPath(path.Name, path.Path, obj)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			if value == nil {
				continue
			}
			values = append(values, *value)
		}
	}

	return values, utilerrors.NewAggregate(errs)
}

func getValueByJsonPath(name, path string, obj *unstructured.Unstructured) (*workapiv1.FeedbackValue, error) {
	j := jsonpath.New(name).AllowMissingKeys(true)
	err := j.Parse(fmt.Sprintf("{.status%s}", path))
	if err != nil {
		return nil, err
	}

	results, err := j.FindResults(obj.UnstructuredContent())

	if err != nil {
		return nil, err
	}

	if len(results) == 0 || len(results[0]) == 0 {
		// no results are found here.
		return nil, nil
	}

	// as we only support simple JSON path, we can assume to have only one result (or none, filtered out above)
	value := results[0][0].Interface()
	var fieldValue workapiv1.FieldValue
	switch t := value.(type) {
	case int64:
		fieldValue = workapiv1.FieldValue{
			Type:    workapiv1.Integer,
			Integer: int32(t),
		}
		return &workapiv1.FeedbackValue{
			Name:  name,
			Value: fieldValue,
		}, nil
	case string:
		fieldValue = workapiv1.FieldValue{
			Type:   workapiv1.String,
			String: t,
		}
		return &workapiv1.FeedbackValue{
			Name:  name,
			Value: fieldValue,
		}, nil
	case bool:
		fieldValue = workapiv1.FieldValue{
			Type:    workapiv1.Boolean,
			Boolean: t,
		}
		return &workapiv1.FeedbackValue{
			Name:  name,
			Value: fieldValue,
		}, nil
	}

	return nil, fmt.Errorf("type for %v is not found", reflect.TypeOf(value))
}
