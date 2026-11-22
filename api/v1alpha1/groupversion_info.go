// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package v1alpha1 holds the KubeStack CRD types. Engine-neutral: no lifecycle-engine fields.
// +kubebuilder:object:generate=true
// +groupName=kubestack.dev
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion  = schema.GroupVersion{Group: "kubestack.dev", Version: "v1alpha1"}
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
	AddToScheme   = SchemeBuilder.AddToScheme
)
