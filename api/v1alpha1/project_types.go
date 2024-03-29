// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/fluxcd/pkg/apis/meta"
	gcv1alpha1 "github.com/open-component-model/git-controller/apis/mpas/v1alpha1"
)

const (
	ProjectFinalizer = "finalizers.mpas.ocm.software"
)

// ExistingRepositoryPolicy defines what to do in case a requested repository already exists.
type ExistingRepositoryPolicy string

// ProjectSpec defines the desired state of Project.
type ProjectSpec struct {
	// +required
	Git gcv1alpha1.RepositorySpec `json:"git"`
	// +optional
	// +kubebuilder:default={interval: "5m"}
	Flux FluxSpec `json:"flux,omitempty"`
	// +optional
	// +kubebuilder:default=true
	Prune bool `json:"prune,omitempty"`
	// +optional
	Interval metav1.Duration `json:"interval,omitempty"`
}

type FluxSpec struct {
	// +optional
	// +kubebuilder:validation:Type=string
	// +kubebuilder:validation:Pattern="^([0-9]+(\\.[0-9]+)?(ms|s|m|h))+$"
	Interval metav1.Duration `json:"interval,omitempty"`
}

// CommitTemplate defines the default commit template for a project if one is not provided in the spec.
type CommitTemplate struct {
	Name    string
	Email   string
	Message string
}

// ProjectStatus defines the observed state of Project.
type ProjectStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// ObservedGeneration is the last reconciled generation of the resource.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Inventory contains the list of Kubernetes resource object references that
	// have been successfully applied.
	// +optional
	Inventory *ResourceInventory `json:"inventory,omitempty"`

	// RepositoryRef contains the reference to the repository resource that has been created by the project controller.
	// +optional
	RepositoryRef *meta.NamespacedObjectReference `json:"repositoryRef,omitempty"`
}

// GetServiceAccountNamespacedName returns the service account namespace name from the inventory.
func (in *Project) GetServiceAccountNamespacedName() (types.NamespacedName, error) {
	// Entry ID: <namespace>_<name>_<group>_<kind>. Just look for a postfix of gitrepository
	if in.Status.Inventory == nil {
		return types.NamespacedName{}, fmt.Errorf("project inventory is empty")
	}

	var name, namespace string
	const nameNamespaceCount = 2
	for _, e := range in.Status.Inventory.Entries {
		split := strings.Split(e.ID, "_")
		if len(split) < nameNamespaceCount {
			return types.NamespacedName{}, fmt.Errorf("failed to split ID: %s", e.ID)
		}

		if split[len(split)-1] == "ServiceAccount" {
			name = split[1]
			namespace = split[0]

			break
		}
	}

	if name == "" {
		return types.NamespacedName{}, fmt.Errorf("service account not found in the project inventory")
	}

	return types.NamespacedName{Name: name, Namespace: namespace}, nil
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=proj
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description=""
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].message",description=""

// Project is the Schema for the projects API.
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

func (in *Project) GetNameWithPrefix(prefix string) string {
	return prefix + "-" + in.Name
}

//+kubebuilder:object:root=true

// ProjectList contains a list of Project.
type ProjectList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Project `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Project{}, &ProjectList{})
}
