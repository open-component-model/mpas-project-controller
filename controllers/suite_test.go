// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"testing"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	gcv1alpha1 "github.com/open-component-model/git-controller/apis/mpas/v1alpha1"
	mpasv1alpha1 "github.com/open-component-model/mpas-project-controller/api/v1alpha1"
	//+kubebuilder:scaffold:imports
)

type testEnv struct {
	scheme *runtime.Scheme
	obj    []client.Object
}

// FakeKubeClientOption defines options to construct a fake kube client. There are some defaults involved.
// Scheme gets corev1 and v1alpha1 schemes by default. Anything that is passed in will override current
// defaults.
type FakeKubeClientOption func(testEnv *testEnv)

func WithAddToScheme(addToScheme func(s *runtime.Scheme) error) FakeKubeClientOption {
	return func(testEnv *testEnv) {
		if err := addToScheme(testEnv.scheme); err != nil {
			panic(err)
		}
	}
}

func WithObjects(obj ...client.Object) FakeKubeClientOption {
	return func(testEnv *testEnv) {
		testEnv.obj = obj
	}
}

func (t *testEnv) FakeKubeClient(opts ...FakeKubeClientOption) client.Client {
	for _, opt := range opts {
		opt(t)
	}

	return fake.NewClientBuilder().WithScheme(t.scheme).WithObjects(t.obj...).Build()
}

var (
	DefaultProject = &mpasv1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-project",
			Namespace: "default",
		},
		Spec: mpasv1alpha1.ProjectSpec{
			Git: gcv1alpha1.RepositorySpec{
				Provider: "github",
				Owner:    "e2e-tester",
				Credentials: gcv1alpha1.Credentials{
					SecretRef: corev1.LocalObjectReference{
						Name: "project-creds",
					},
				},
				Visibility:               "public",
				Domain:                   "github.com",
				ExistingRepositoryPolicy: "adopt",
			},
			Prune: true,
		},
	}
)

var env *testEnv

func TestMain(m *testing.M) {
	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)
	_ = rbacv1.AddToScheme(scheme)
	_ = sourcev1.AddToScheme(scheme)
	_ = kustomizev1.AddToScheme(scheme)
	_ = mpasv1alpha1.AddToScheme(scheme)
	_ = gcv1alpha1.AddToScheme(scheme)
	_ = certmanagerv1.AddToScheme(scheme)

	env = &testEnv{
		scheme: scheme,
	}
	m.Run()
}
