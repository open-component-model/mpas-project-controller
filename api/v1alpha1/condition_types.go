package v1alpha1

// Project controller condition types.
const (
	// NamespaceReadyCondition indicates that the project namespace
	// has been created and is ready to use.
	// If true, the namespace is ready to use.
	// If false, the namespace failed to be created.
	// This condition only exists if a namespace creation has been attempted.
	NamespaceReadyCondition string = "NamespaceReady"

	// RepositoryReadyCondition indicates that the project repository
	// has been created and is ready to use.
	// If true, the repository is ready to use.
	// This condition is only present on the resource if the repository
	// has been successfully created.
	RepositoryReadyCondition string = "RepositoryReady"

	// ServiceAccountReadyCondition indicates that the project service account
	// has been created and is ready to use.
	// If true, the service account is ready to use.
	// This condition is only present on the resource if the service account
	// has been successfully created.
	ServiceAccountReadyCondition string = "ServiceAccountReady"

	// RBACReadyCondition indicates that the project RBAC
	// has been created and is ready to use.
	// If true, the RBAC is ready to use.
	// This condition is only present on the resource if the RBAC
	// has been successfully created.
	RBACReadyCondition string = "RBACReady"

	// FluxResourcesReadyCondition indicates that the project Flux resources
	// have been created and is ready to use.
	// If true, the Flux resources are ready to use.
	// This condition is only present on the resource if the Flux resources
	// have been successfully created.
	FluxResourcesReadyCondition string = "FluxResourcesReady"
)

const (
	// NamespaceCreationFailedReason indicates that the project namespace could not be created.
	NamespaceCreationFailedReason string = "NamespaceCreationFailed"
)
