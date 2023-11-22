package controllers

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/open-component-model/mpas-project-controller/api/v1alpha1"
)

var errNotProjectNamespace = errors.New("not in a namespace that belongs to a project")

// GetProjectFromObjectNamespace returns the Project from the annotation of the current namespace that an object
// is in.
func (r *SecretsReconciler) GetProjectFromObjectNamespace(ctx context.Context, c client.Client, obj client.Object) (*v1alpha1.Project, error) {
	// Look up the namespace of the object and check the annotation.
	ns := &corev1.Namespace{}
	if err := c.Get(ctx, types.NamespacedName{Name: obj.GetNamespace()}, ns); err != nil {
		return nil, fmt.Errorf("failed to retrieve namespace for object: %w", err)
	}

	v, ok := ns.Labels[v1alpha1.ProjectKey]
	if !ok {
		return nil, errNotProjectNamespace
	}

	// Get the project from the annotation.
	project := &v1alpha1.Project{}
	if err := c.Get(ctx, types.NamespacedName{Name: v, Namespace: r.DefaultNamespace}, project); err != nil {
		return nil, fmt.Errorf("failed to find project in namespace: %w", err)
	}

	return project, nil
}
