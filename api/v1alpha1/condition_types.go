package v1alpha1

const (
	WaitingOnResourcesReason string = "WaitingOnResources"

	// NamespaceCreateOrUpdateFailedReason indicates that the project namespace could not be reconciled.
	NamespaceCreateOrUpdateFailedReason string = "NamespaceCreateOrUpdateFailed"

	// ServiceAccountCreateOrUpdateFailedReason indicates that the project service account could not be reconciled.
	ServiceAccountCreateOrUpdateFailedReason string = "ServiceAccountCreateOrUpdateFailed"

	// RBACCreateOrUpdateFailedReason indicates that the project cluster role could not be reconciled.
	RBACCreateOrUpdateFailedReason string = "RBACCreateOrUpdateFailed"

	// RepositoryCreateOrUpdateFailedReason indicates that the project repository could not be reconciled.
	RepositoryCreateOrUpdateFailedReason string = "RepositoryCreateOrUpdateFailed"

	// FluxGitRepositoryCreateOrUpdateFailedReason indicates that the project Flux GitRepository source could not be reconciled.
	FluxGitRepositoryCreateOrUpdateFailedReason string = "FluxGitRepositoryCreateOrUpdateFailed"

	// FluxKustomizationsCreateOrUpdateFailedReason indicates that the project Flux Kustomizations could not be reconciled.
	FluxKustomizationsCreateOrUpdateFailedReason string = "FluxKustomizationsCreateOrUpdateFailed"

	// ReconciliationFailedReason represents the fact that the reconciliation failed.
	ReconciliationFailedReason string = "ReconciliationFailed"
)
