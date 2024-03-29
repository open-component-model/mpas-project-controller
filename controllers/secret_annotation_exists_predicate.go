// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"github.com/open-component-model/mpas-project-controller/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// SecretAnnotationExistsPredicate watches a subscription for reconciled version changes.
type SecretAnnotationExistsPredicate struct {
	predicate.Funcs
}

// Update will check if the new secret contains the managed annotation.
func (SecretAnnotationExistsPredicate) Update(e event.UpdateEvent) bool {
	if e.ObjectOld == nil || e.ObjectNew == nil {
		return false
	}

	oldSecret, ok := e.ObjectOld.(*corev1.Secret)
	if !ok {
		return false
	}

	newSecret, ok := e.ObjectNew.(*corev1.Secret)
	if !ok {
		return false
	}

	_, oldOk := oldSecret.Annotations[v1alpha1.ManagedMPASSecretAnnotationKey]
	_, newOk := newSecret.Annotations[v1alpha1.ManagedMPASSecretAnnotationKey]

	return (oldOk && !newOk) || newOk
}

// Create will check if the secret contains the managed annotation.
func (SecretAnnotationExistsPredicate) Create(e event.CreateEvent) bool {
	return checkAnnotation(e.Object)
}

// Delete will make sure we don't remove anything that doesn't have the right mpas annotation.
func (SecretAnnotationExistsPredicate) Delete(e event.DeleteEvent) bool {
	return checkAnnotation(e.Object)
}

func checkAnnotation(obj client.Object) bool {
	if obj == nil {
		return false
	}

	secret, ok := obj.(*corev1.Secret)
	if !ok {
		return false
	}

	_, ok = secret.Annotations[v1alpha1.ManagedMPASSecretAnnotationKey]

	return ok
}
