package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/fluxcd/pkg/runtime/conditions"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	kuberecorder "k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/open-component-model/mpas-project-controller/api/v1alpha1"
)

// SecretsReconciler reconciles a Secret object.
type SecretsReconciler struct {
	client.Client
	kuberecorder.EventRecorder

	Scheme           *runtime.Scheme
	DefaultNamespace string
}

// SetupWithManager sets up the controller with the Manager.
func (r *SecretsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}, &SecretAnnotationExistsPredicate{})).
		Complete(r)
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *SecretsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, retErr error) {
	logger := log.FromContext(ctx)
	logger.Info("reconciling object", "secret", req.NamespacedName)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      req.Name,
			Namespace: req.Namespace,
		},
	}

	project, err := r.GetProjectFromObjectNamespace(ctx, r.Client, secret)
	if err != nil {
		if errors.Is(err, errNotProjectNamespace) {
			logger.Info("secret belongs to a namespace that was not created by project controller... ignoring")

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to find project in namespace %s: %w", secret.Namespace, err)
	}

	if !conditions.IsReady(project) {
		logger.Info("waiting for project to become ready...")

		return ctrl.Result{RequeueAfter: project.GetRequeueAfter()}, nil
	}

	serviceAccount := &corev1.ServiceAccount{}
	key, err := project.GetServiceAccountNamespacedName()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to find project service account in inventory: %w", err)
	}

	if err := r.Get(ctx, key, serviceAccount); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to fetch service account: %w", err)
	}

	origServiceAccount := serviceAccount.DeepCopy()

	defer func() {
		// only update if there is a need for it
		if reflect.DeepEqual(origServiceAccount.ImagePullSecrets, serviceAccount.ImagePullSecrets) {
			return
		}

		logger.Info("updating service account", "secrets", serviceAccount.ImagePullSecrets)
		reason := v1alpha1.AddServiceAccountImagePullSecretsReason
		if len(origServiceAccount.ImagePullSecrets) > len(serviceAccount.ImagePullSecrets) {
			reason = v1alpha1.RemoveServiceAccountImagePullSecretsReason
		}
		r.EventRecorder.Event(serviceAccount, v1alpha1.UpdateServiceAccountImagePullSecretsType, reason, "")

		if err := r.Update(ctx, serviceAccount); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("failed to update service account: %w", err))
		}
	}()

	// if not found or deleted, reconcile deleted -> remove from list if still in there
	if err := r.Get(ctx, req.NamespacedName, secret); err != nil {
		if apierrors.IsNotFound(err) {
			// make sure we don't have it in our list of image pull secrets.
			r.reconcileDelete(ctx, serviceAccount, secret)

			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to fetch secret from cluster: %w", err)
	}

	// reconcile delete
	if secret.DeletionTimestamp != nil {
		r.reconcileDelete(ctx, serviceAccount, secret)

		return ctrl.Result{}, nil
	}

	r.reconcileNormal(ctx, serviceAccount, secret)

	// reconcile normal
	return ctrl.Result{}, nil
}

func (r *SecretsReconciler) reconcileNormal(ctx context.Context, account *corev1.ServiceAccount, secret *corev1.Secret) {
	logger := log.FromContext(ctx)

	logger.Info("reconciling secret to image pull secrets.")
	if r.containsSecret(account.ImagePullSecrets, secret.Name) {
		// If the annotation was deleted but the secret is IN the list of secrets, remove it.
		if _, ok := secret.Annotations[v1alpha1.ManagedMPASSecretAnnotationKey]; !ok {
			r.deleteSecret(account, secret)
		}

		logger.Info("nothing to do, secret already added to image pull secrets")

		return
	}

	account.ImagePullSecrets = append(account.ImagePullSecrets, corev1.LocalObjectReference{Name: secret.Name})
}

func (r *SecretsReconciler) reconcileDelete(ctx context.Context, account *corev1.ServiceAccount, secret *corev1.Secret) {
	logger := log.FromContext(ctx)
	if !r.containsSecret(account.ImagePullSecrets, secret.Name) {
		// nothing to do, secret already removed from service account
		logger.Info("nothing to do, secret already removed from image pull secrets")

		return
	}

	r.deleteSecret(account, secret)
}

func (r *SecretsReconciler) deleteSecret(account *corev1.ServiceAccount, secret *corev1.Secret) {
	pullSecrets := account.ImagePullSecrets
	for i := 0; i < len(pullSecrets); i++ {
		if pullSecrets[i].Name == secret.Name {
			pullSecrets = append(pullSecrets[:i], pullSecrets[i+1:]...)

			break
		}
	}

	account.ImagePullSecrets = pullSecrets
}

func (r *SecretsReconciler) containsSecret(list []corev1.LocalObjectReference, name string) bool {
	for _, ref := range list {
		if ref.Name == name {
			return true
		}
	}

	return false
}
