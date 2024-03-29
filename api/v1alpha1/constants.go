// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

const (
	// ProjectKey contains the name of the project for this namespace.
	// This key is used to look up the Project that belongs to it.
	ProjectKey = "mpas.ocm.system/project"
)

const (
	// ManagedMPASSecretAnnotationKey denotes that the project controller needs to set these secrets
	// in the service account of the project.
	ManagedMPASSecretAnnotationKey = "mpas.ocm.system/secret.dockerconfig" //nolint:gosec // not a cred
)
