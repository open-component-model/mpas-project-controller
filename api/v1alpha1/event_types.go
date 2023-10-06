package v1alpha1

const (
	// UpdateServiceAccountImagePullSecretsType used when updating a service account by the secret controller.
	UpdateServiceAccountImagePullSecretsType = "Normal"
	// AddServiceAccountImagePullSecretsReason defines the reason why the update occurred.
	AddServiceAccountImagePullSecretsReason = "ImagePullSecretAdded"
	// RemoveServiceAccountImagePullSecretsReason defines the reason why the update occurred.
	RemoveServiceAccountImagePullSecretsReason = "ImagePullSecretRemoved"
)
