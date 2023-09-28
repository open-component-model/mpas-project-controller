package controllers

import (
	"context"
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/open-component-model/mpas-project-controller/api/v1alpha1"
)

var notProject = errors.New("no project in namespace")

// GetProjectInNamespace returns the Project in the current namespace.
func GetProjectInNamespace(ctx context.Context, c client.Client, namespace string) (*v1alpha1.Project, error) {
	projectList := &v1alpha1.ProjectList{}
	if err := c.List(ctx, projectList, client.InNamespace(namespace)); err != nil {
		return nil, fmt.Errorf("failed to find project in namespace: %w", err)
	}

	if v := len(projectList.Items); v != 1 {
		return nil, fmt.Errorf("exactly one Project should have been found in namespace %s; got: %d: %w", namespace, v, notProject)
	}

	project := &projectList.Items[0]

	return project, nil
}
