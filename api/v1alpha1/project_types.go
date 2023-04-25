// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ProjectSpec defines the desired state of Project
type ProjectSpec struct {
	// +required
	Git Git `json:"git"`
	// +optional
	Interval metav1.Duration `json:"interval,omitempty"`
}

type Git struct {
	// Provider is the name of the git provider, e.g. github, gitlab, bitbucket
	// +required
	Provider string `json:"provider"`

	// Owner is the name of the owner of the repository, e.g. open-component-model
	// +required
	Owner string `json:"owner"`

	// IsOrganization specifies if the owner is an organization or a user
	// +optional
	// +kubebuilder:default=false
	IsOganization bool `json:"isOrganization"`

	// Repository contains the details of the repository
	// +required
	Repository Repository `json:"repository"`

	// Credentials contains a reference to the secret containing the credentials
	// +required
	Credentials Credentials `json:"credentials"`
}

type Repository struct {
	// Name is the name of the repository, e.g. mpas-project-controller
	// +required
	Name string `json:"name"`

	// Maintainers is a list of maintainers of the repository
	// +required
	Maintainers []string `json:"maintainers"`

	// Visibility is the visibility of the repository, must be one of public, private, or internal.
	// Default is private
	// +optional
	// +kubebuilder:validation:Enum=public;private;internal
	// +kubebuilder:default=private
	Visibility string `json:"visibility"`

	// ExistingReposityPolicy specifies the policy for existing repositories, must be one of adopt or fail
	// Default is adopt
	// +optional
	// +kubebuilder:validation:Enum=adopt;fail
	// +kubebuilder:default=adopt
	ExistingReposityPolicy string `json:"existingReposityPolicy"`
}

type Credentials struct {
	SecretRef meta.LocalObjectReference `json:"secretRef"`
}

// ProjectStatus defines the observed state of Project
type ProjectStatus struct {
	// +optional
	// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description=""
	// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description=""
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the last reconciled generation of the resource.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Project is the Schema for the projects API
type Project struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ProjectSpec   `json:"spec,omitempty"`
	Status ProjectStatus `json:"status,omitempty"`
}

// GetConditions returns the conditions of the Project.
func (in *Project) GetConditions() []metav1.Condition {
	return in.Status.Conditions
}

// SetConditions sets the conditions of the Project.
func (in *Project) SetConditions(conditions []metav1.Condition) {
	in.Status.Conditions = conditions
}

// GetRequeueAfter returns the requeue time of the Project.
func (in *Project) GetRequeueAfter() time.Duration {
	return in.Spec.Interval.Duration
}

//+kubebuilder:object:root=true

// ProjectList contains a list of Project
type ProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Project `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Project{}, &ProjectList{})
}
