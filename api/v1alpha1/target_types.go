// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TargetSpec defines the desired state of Target
type TargetSpec struct {
	// Type specifies the type of the target. Possible values are: kubernetes, ssh, ociRepository
	// +required
	// +kubebuilder:validation:Enum=kubernetes;ssh;ociRepository
	Type string `json:"type"`

	Access *Access `json:"access,omitempty"`
}

// TargetStatus defines the observed state of Target
type TargetStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// Access defines the access information for a target
type Access struct {
	// +required
	SecretRef *TargetSecretRef `json:"secretRef"`
}

// TargetSecretRef defines the reference to a secret within the cluster
type TargetSecretRef struct {
	// +required
	Name string `json:"name"`

	// Should we specify a default value?
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Target is the Schema for the targets API
type Target struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TargetSpec   `json:"spec,omitempty"`
	Status TargetStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// TargetList contains a list of Target
type TargetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Target `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Target{}, &TargetList{})
}
