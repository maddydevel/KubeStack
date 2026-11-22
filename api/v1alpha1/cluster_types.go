// Copyright 2026 The KubeStack Authors. All rights reserved.

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

const (
	DeletionRetain = "Retain" // deleting the resource only stops KubeStack managing the cluster
	DeletionDelete = "Delete" // deleting the resource deletes (provisioned) or disconnects (attached) the cluster

	ConditionSynced = "Synced"
)

// +kubebuilder:validation:XValidation:rule="self.tier != 'provisioned' || has(self.kubernetesVersion)",message="kubernetesVersion is required for provisioned clusters"
type KubeStackClusterSpec struct {
	// provisioned: KubeStack creates an RKE2 cluster. attached: an existing cluster is connected.
	// +kubebuilder:validation:Enum=provisioned;attached
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="tier is immutable"
	Tier string `json:"tier"`
	// Desired Kubernetes version (provisioned only). Changing it upgrades the cluster after pre-flight checks.
	// +optional
	KubernetesVersion string `json:"kubernetesVersion,omitempty"`
	// What deleting this resource does to the cluster. Delete must be set explicitly.
	// +kubebuilder:validation:Enum=Retain;Delete
	// +kubebuilder:default=Retain
	// +optional
	DeletionPolicy string `json:"deletionPolicy,omitempty"`
}

type KubeStackClusterStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// +optional
	Tier string `json:"tier,omitempty"`
	// +optional
	UpgradeOwner string `json:"upgradeOwner,omitempty"`
	// +optional
	KubernetesVersion string `json:"kubernetesVersion,omitempty"`
	// +optional
	Ready bool `json:"ready,omitempty"`
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// KubeStackCluster is a cluster managed by KubeStack. Its name is the cluster ID.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope=Cluster,shortName=ksc
// +kubebuilder:printcolumn:name="Tier",type=string,JSONPath=`.spec.tier`
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=`.status.kubernetesVersion`
// +kubebuilder:printcolumn:name="Ready",type=boolean,JSONPath=`.status.ready`
// +kubebuilder:printcolumn:name="Synced",type=string,JSONPath=`.status.conditions[?(@.type=="Synced")].status`
type KubeStackCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubeStackClusterSpec   `json:"spec,omitempty"`
	Status KubeStackClusterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type KubeStackClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeStackCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeStackCluster{}, &KubeStackClusterList{})
}
