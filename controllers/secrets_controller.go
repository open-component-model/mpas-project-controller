package controllers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/fluxcd/pkg/runtime/conditions"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// SecretsReconciler reconciles a Secret object
type SecretsReconciler struct {
	client.Client
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
		if apierrors.IsNotFound(err) || errors.Is(err, notProject) {
			// silently skip if project is not there yet.
			// TODO: this is a problem if things are assigned simultaneously.
			logger.Info("project not found in mpas namespace; requeuing so we don't miss the secret")
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to find project in namespace %s: %w", r.DefaultNamespace, err)
	}

	if !conditions.IsReady(project) {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	serviceAccount := &corev1.ServiceAccount{}
	key, err := project.GetServiceAccountNamespacedName()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to find project service account in inventory: %w", err)
	}

	if err := r.Get(ctx, key, serviceAccount); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to fetch service account: %w", err)
	}

	defer func() {
		logger.Info("updating service account", "secrets", serviceAccount.ImagePullSecrets)
		if err := r.Update(ctx, serviceAccount); err != nil {
			retErr = errors.Join(retErr, fmt.Errorf("failed to update service account: %w", err))
		}
	}()

	// if not found or deleted, reconcile deleted -> remove from list if still in there
	if err := r.Get(ctx, req.NamespacedName, secret); err != nil {
		if apierrors.IsNotFound(err) {
			// make sure we don't have it in our list of image pull secrets.
			return r.reconcileDelete(ctx, serviceAccount, req.NamespacedName)
		}

		return ctrl.Result{}, fmt.Errorf("failed to fetch secret from cluster: %w", err)
	}

	// reconcile delete
	if secret.DeletionTimestamp != nil {
		return r.reconcileDelete(ctx, serviceAccount, req.NamespacedName)
	}

	// reconcile normal
	return r.reconcileNormal(ctx, serviceAccount, req.NamespacedName)
}

func (r *SecretsReconciler) reconcileNormal(ctx context.Context, account *corev1.ServiceAccount, req types.NamespacedName) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("appending secret to image pull secrets.")
	if r.containsSecret(account.ImagePullSecrets, req.Name) {
		logger.Info("nothing to do, secret already contained in image pull secrets")

		return ctrl.Result{}, nil
	}

	account.ImagePullSecrets = append(account.ImagePullSecrets, corev1.LocalObjectReference{Name: req.Name})

	return ctrl.Result{}, nil
}

func (r *SecretsReconciler) reconcileDelete(ctx context.Context, account *corev1.ServiceAccount, name types.NamespacedName) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	if !r.containsSecret(account.ImagePullSecrets, name.Name) {
		// nothing to do, secret already removed from service account
		logger.Info("nothing to do, secret already removed from image pull secrets")

		return ctrl.Result{}, nil
	}

	pullSecrets := account.ImagePullSecrets
	for i := 0; i < len(pullSecrets); i++ {
		if pullSecrets[i].Name == name.Name {
			pullSecrets = append(pullSecrets[:i], pullSecrets[i+1:]...)
			break
		}
	}

	account.ImagePullSecrets = pullSecrets

	return ctrl.Result{}, nil
}

func (r *SecretsReconciler) containsSecret(list []corev1.LocalObjectReference, name string) bool {
	for _, ref := range list {
		if ref.Name == name {
			return true
		}
	}

	return false
}
