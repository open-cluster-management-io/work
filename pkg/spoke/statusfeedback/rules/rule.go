package rules

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

type WellKnownStatusRuleResolver interface {
	GetPathsByKind(schema.GroupKind) []workapiv1.JsonPath
}

type DefaultWellKnownStatusResolver struct {
	rules map[schema.GroupKind][]workapiv1.JsonPath
}

var deploymentRule = []workapiv1.JsonPath{
	{
		Name: "ReadyReplicas",
		Path: ".readyReplicas",
	},
	{
		Name: "Replicas",
		Path: ".replicas",
	},
	{
		Name: "AvailableReplicas",
		Path: ".availableReplicas",
	},
}

func DefaultWellKnownStatusRule() WellKnownStatusRuleResolver {
	return &DefaultWellKnownStatusResolver{
		rules: map[schema.GroupKind][]workapiv1.JsonPath{
			{Group: "apps", Kind: "Deployment"}: deploymentRule,
		},
	}
}

func (w *DefaultWellKnownStatusResolver) GetPathsByKind(gvk schema.GroupKind) []workapiv1.JsonPath {
	return w.rules[gvk]
}
