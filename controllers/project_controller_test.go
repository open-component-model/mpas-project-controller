package controllers

import (
	"context"
	"testing"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	mpasv1alpha1 "github.com/open-component-model/mpas-project-controller/api/v1alpha1"
)

func TestProjectReconciler(t *testing.T) {
	project := DefaultProject.DeepCopy()
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "project-creds",
			Namespace: "default",
		},
		Data: map[string][]byte{
			"username": []byte("test-user"),
			"password": []byte("test-password"),
		},
	}
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mpas-projects-clusterrole",
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{""},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{"mpas.ocm.software"},
				Resources: []string{"Repository"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		},
	}

	controllerutil.AddFinalizer(project, mpasv1alpha1.ProjectFinalizer)

	client := env.FakeKubeClient(WithAddToScheme(mpasv1alpha1.AddToScheme), WithObjects(project, secret, cr))
	controller := &ProjectReconciler{
		Client:          client,
		Scheme:          env.scheme,
		ClusterRoleName: cr.Name,
	}

	_, err := controller.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: project.Namespace,
			Name:      project.Name,
		},
	})
	require.NoError(t, err)

	// Reconcile twice because the project will be requeued to wait for resources to be created.
	_, err = controller.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: project.Namespace,
			Name:      project.Name,
		},
	})
	require.NoError(t, err)

	err = client.Get(context.Background(), types.NamespacedName{
		Namespace: project.Namespace,
		Name:      project.Name,
	}, project)
	require.NoError(t, err)

	assert.True(t, conditions.IsTrue(project, meta.ReadyCondition))
}
