// Copyright (c) Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package features

import (
	"k8s.io/apimachinery/pkg/util/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/component-base/featuregate"
)

const (
	// Every feature gate should add method here following this template:
	//
	// // owner: @username
	// // alpha: v1.X
	// MyFeature featuregate.Feature = "MyFeature"

	// NilExecutorValidating will validate the manifest work even if its executor is nil, it will check if the request
	// user has the execute-as permission with the default executor "system:serviceaccount::klusterlet-work-sa"
	// TODO: move to api repo
	NilExecutorValidating featuregate.Feature = "NilExecutorValidating"
)

func init() {
	runtime.Must(utilfeature.DefaultMutableFeatureGate.Add(DefaultWebhookFeatureGates))
}

// DefaultWebhookFeatureGates consists of all known acm-importing feature keys.
// To add a new feature, define a key for it above and add it here.
var DefaultWebhookFeatureGates = map[featuregate.Feature]featuregate.FeatureSpec{
	NilExecutorValidating: {Default: false, PreRelease: featuregate.Alpha},
}
