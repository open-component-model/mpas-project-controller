package controllers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/open-component-model/mpas-project-controller/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	controllerruntime "sigs.k8s.io/controller-runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSecretsReconciler_Reconcile(t *testing.T) {
	tests := []struct {
		name       string
		client     func() client.Client
		secretName string
		want       controllerruntime.Result
		wantErr    assert.ErrorAssertionFunc
		wantResult assert.ValueAssertionFunc
	}{
		{
			name:       "adds secrets to service account with label",
			secretName: "test-secret",
			client: func() client.Client {
				serviceAccount := &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service-account",
						Namespace: "mpas-system",
					},
				}
				project := &v1alpha1.Project{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-project",
						Namespace: "mpas-system",
					},
					Status: v1alpha1.ProjectStatus{
						Inventory: &v1alpha1.ResourceInventory{
							Entries: []v1alpha1.ResourceRef{
								{
									ID:      "mpas-system_test-service-account_v1_ServiceAccount",
									Version: "1",
								},
							},
						},
					},
				}
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "mpas-system",
						Annotations: map[string]string{
							"mpas.ocm.system/secret.dockerconfig": "managed",
						},
					},
					Data: map[string][]byte{
						"bla": []byte("bla"),
					},
					Type: "generic",
				}
				conditions.MarkTrue(project, meta.ReadyCondition, meta.SucceededReason, "Done")
				fakeClient := env.FakeKubeClient(WithObjects(project, secret, serviceAccount))

				return fakeClient
			},
			wantResult: func(t assert.TestingT, a any, b ...any) bool {
				serviceAccount := a.(*corev1.ServiceAccount)
				for _, s := range serviceAccount.ImagePullSecrets {
					if s.Name == "test-secret" {
						return true
					}
				}

				return assert.Fail(t, "Expected test-secret to be contained in the service account.")
			},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				if err != nil {
					assert.Fail(t, fmt.Sprintf("Expected no error, but error occurred: %v", err))
				}

				return false
			},
		},
		{
			name:       "deleted secrets are removed from the service account secret list",
			secretName: "test-secret-2",
			client: func() client.Client {
				serviceAccount := &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service-account",
						Namespace: "mpas-system",
					},
					ImagePullSecrets: []corev1.LocalObjectReference{
						{
							Name: "test-secret-2",
						},
					},
				}
				project := &v1alpha1.Project{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-project",
						Namespace: "mpas-system",
					},
					Status: v1alpha1.ProjectStatus{
						Inventory: &v1alpha1.ResourceInventory{
							Entries: []v1alpha1.ResourceRef{
								{
									ID:      "mpas-system_test-service-account_v1_ServiceAccount",
									Version: "1",
								},
							},
						},
					},
				}
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret-2",
						Namespace: "mpas-system",
						Annotations: map[string]string{
							"mpas.ocm.system/secret.dockerconfig": "managed",
						},
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
					},
					Data: map[string][]byte{
						"bla": []byte("bla"),
					},
					Type: "generic",
				}
				conditions.MarkTrue(project, meta.ReadyCondition, meta.SucceededReason, "Done")
				fakeClient := env.FakeKubeClient(WithObjects(project, secret, serviceAccount))

				return fakeClient
			},
			wantResult: func(t assert.TestingT, a any, b ...any) bool {
				serviceAccount := a.(*corev1.ServiceAccount)
				for _, s := range serviceAccount.ImagePullSecrets {
					if s.Name == "test-secret-2" {
						return assert.Fail(t, "Did not expect test-secret-2 to be in the service account image pull secrets.")
					}
				}

				return true
			},
			wantErr: func(t assert.TestingT, err error, i ...interface{}) bool {
				if err != nil {
					return assert.Fail(t, fmt.Sprintf("Expected no error, but error occurred: %v", err))
				}

				return false
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.client()
			r := &SecretsReconciler{
				Client:           c,
				Scheme:           env.scheme,
				DefaultNamespace: "mpas-system",
			}
			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: tt.secretName, Namespace: "mpas-system"}})
			tt.wantErr(t, err, fmt.Sprintf("Reconcile(%v)", tt.name))

			serviceAccount := &corev1.ServiceAccount{}
			require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "test-service-account", Namespace: "mpas-system"}, serviceAccount))

			tt.wantResult(t, serviceAccount)
		})
	}
}
