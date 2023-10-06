package controllers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kuberecorder "k8s.io/client-go/tools/record"
	controllerruntime "sigs.k8s.io/controller-runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/open-component-model/mpas-project-controller/api/v1alpha1"
)

func TestSecretsReconciler_Reconcile(t *testing.T) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-namespace",
			Annotations: map[string]string{
				v1alpha1.ProjectKey: "test-project",
			},
		},
	}
	mpasSystem := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "mpas-system",
		},
	}

	tests := []struct {
		name       string
		client     func() client.Client
		secretName string
		want       controllerruntime.Result
		wantErr    assert.ErrorAssertionFunc
		wantResult assert.ValueAssertionFunc
		wantEvent  assert.BoolAssertionFunc
	}{
		{
			name:       "adds secrets to service account with label",
			secretName: "test-secret",
			client: func() client.Client {
				serviceAccount := &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service-account",
						Namespace: "test-namespace",
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
									ID:      "test-namespace_test-service-account_v1_ServiceAccount",
									Version: "1",
								},
							},
						},
					},
				}
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret",
						Namespace: "test-namespace",
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
				fakeClient := env.FakeKubeClient(WithObjects(mpasSystem, ns, project, secret, serviceAccount))

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
			wantErr: func(t assert.TestingT, err error, i ...any) bool {
				if err != nil {
					assert.Fail(t, fmt.Sprintf("Expected no error, but error occurred: %v", err))
				}

				return false
			},
			wantEvent: func(t assert.TestingT, b bool, i ...any) bool {
				if !b {
					return assert.Fail(t, "Expected events recorder to be called but was not")
				}

				return true
			},
		},
		{
			name:       "deleted secrets are removed from the service account secret list",
			secretName: "test-secret-2",
			client: func() client.Client {
				serviceAccount := &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service-account",
						Namespace: ns.Name,
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
									ID:      "test-namespace_test-service-account_v1_ServiceAccount",
									Version: "1",
								},
							},
						},
					},
				}
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret-2",
						Namespace: ns.Name,
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
				fakeClient := env.FakeKubeClient(WithObjects(mpasSystem, ns, project, secret, serviceAccount))

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
			wantErr: func(t assert.TestingT, err error, i ...any) bool {
				if err != nil {
					return assert.Fail(t, fmt.Sprintf("Expected no error, but error occurred: %v", err))
				}

				return false
			},
			wantEvent: func(t assert.TestingT, b bool, i ...any) bool {
				if !b {
					return assert.Fail(t, "Expected events recorder to be called but was not")
				}

				return true
			},
		},
		{
			name:       "removed annotations are also deleted from the service account",
			secretName: "test-secret-3",
			client: func() client.Client {
				serviceAccount := &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service-account",
						Namespace: ns.Name,
					},
					ImagePullSecrets: []corev1.LocalObjectReference{
						{
							Name: "test-secret-3",
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
									ID:      "test-namespace_test-service-account_v1_ServiceAccount",
									Version: "1",
								},
							},
						},
					},
				}
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "test-secret-3",
						Namespace:         ns.Name,
						DeletionTimestamp: &metav1.Time{Time: time.Now()},
					},
					Data: map[string][]byte{
						"bla": []byte("bla"),
					},
					Type: "generic",
				}
				conditions.MarkTrue(project, meta.ReadyCondition, meta.SucceededReason, "Done")
				fakeClient := env.FakeKubeClient(WithObjects(mpasSystem, ns, project, secret, serviceAccount))

				return fakeClient
			},
			wantResult: func(t assert.TestingT, a any, b ...any) bool {
				serviceAccount := a.(*corev1.ServiceAccount)
				for _, s := range serviceAccount.ImagePullSecrets {
					if s.Name == "test-secret-3" {
						return assert.Fail(t, "Did not expect test-secret-3 to be in the service account image pull secrets.")
					}
				}

				return true
			},
			wantErr: func(t assert.TestingT, err error, i ...any) bool {
				if err != nil {
					return assert.Fail(t, fmt.Sprintf("Expected no error, but error occurred: %v", err))
				}

				return false
			},
			wantEvent: func(t assert.TestingT, b bool, i ...any) bool {
				if !b {
					return assert.Fail(t, "Expected events recorder to be called but was not")
				}

				return true
			},
		},
		{
			name:       "nothing happens if secret list hasn't been updated",
			secretName: "test-secret-4",
			client: func() client.Client {
				serviceAccount := &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service-account",
						Namespace: ns.Name,
					},
					ImagePullSecrets: []corev1.LocalObjectReference{
						{
							Name: "test-secret-4",
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
									ID:      "test-namespace_test-service-account_v1_ServiceAccount",
									Version: "1",
								},
							},
						},
					},
				}
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret-4",
						Namespace: ns.Name,
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
				fakeClient := env.FakeKubeClient(WithObjects(mpasSystem, ns, project, secret, serviceAccount))

				return fakeClient
			},
			wantResult: func(t assert.TestingT, a any, b ...any) bool {
				serviceAccount := a.(*corev1.ServiceAccount)
				for _, s := range serviceAccount.ImagePullSecrets {
					if s.Name == "test-secret-4" {
						return true
					}
				}

				return assert.Fail(t, "Did not find test-secret-4 in the service account image pull secrets.")
			},
			wantErr: func(t assert.TestingT, err error, i ...any) bool {
				if err != nil {
					return assert.Fail(t, fmt.Sprintf("Expected no error, but error occurred: %v", err))
				}

				return false
			},
			wantEvent: func(t assert.TestingT, b bool, i ...any) bool {
				if b {
					return assert.Fail(t, "Unexpected call to event recorder. No modifications should have been applied to the service account.")
				}

				return false
			},
		},
		{
			name:       "ignores secrets that are in a namespace without annotation",
			secretName: "test-secret-5",
			client: func() client.Client {
				serviceAccount := &corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-service-account",
						Namespace: ns.Name,
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
									ID:      "test-namespace_test-service-account_v1_ServiceAccount",
									Version: "1",
								},
							},
						},
					},
				}
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-secret-5",
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
				fakeClient := env.FakeKubeClient(WithObjects(mpasSystem, ns, project, secret, serviceAccount))

				return fakeClient
			},
			wantResult: func(t assert.TestingT, a any, b ...any) bool {
				serviceAccount := a.(*corev1.ServiceAccount)
				for _, s := range serviceAccount.ImagePullSecrets {
					if s.Name == "test-secret-5" {
						return assert.Fail(t, "Did expect not test-secret-5 in the service account image pull secrets.")
					}
				}

				return true
			},
			wantErr: func(t assert.TestingT, err error, i ...any) bool {
				if err != nil {
					return assert.Fail(t, fmt.Sprintf("Expected no error, but error occurred: %v", err))
				}

				return false
			},
			wantEvent: func(t assert.TestingT, b bool, i ...any) bool {
				if b {
					return assert.Fail(t, "Unexpected call to event recorder. No modifications should have been applied to the service account.")
				}

				return false
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.client()
			recorder := &mockEventRecorder{}
			r := &SecretsReconciler{
				Client:           c,
				Scheme:           env.scheme,
				DefaultNamespace: "mpas-system",
				EventRecorder:    recorder,
			}
			_, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: types.NamespacedName{Name: tt.secretName, Namespace: ns.Name}})
			tt.wantErr(t, err, fmt.Sprintf("Reconcile(%v)", tt.name))

			serviceAccount := &corev1.ServiceAccount{}
			require.NoError(t, c.Get(context.Background(), types.NamespacedName{Name: "test-service-account", Namespace: ns.Name}, serviceAccount))

			tt.wantResult(t, serviceAccount)
			tt.wantEvent(t, recorder.called)
		})
	}
}

type mockEventRecorder struct {
	called bool
}

func (m *mockEventRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	m.called = true
}

func (m *mockEventRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...any) {
}

func (m *mockEventRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...any) {
}

var _ kuberecorder.EventRecorder = &mockEventRecorder{}
