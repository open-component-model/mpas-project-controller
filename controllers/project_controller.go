// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"errors"
	"fmt"
	"time"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/fluxcd/pkg/runtime/patch"
	rreconcile "github.com/fluxcd/pkg/runtime/reconcile"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	gcv1alpha1 "github.com/open-component-model/git-controller/apis/mpas/v1alpha1"
	mpasv1alpha1 "github.com/open-component-model/mpas-project-controller/api/v1alpha1"
)

// ProjectReconciler reconciles a Project object
type ProjectReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=mpas.ocm.software,resources=projects,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mpas.ocm.software,resources=projects/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mpas.ocm.software,resources=projects/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var (
		retErr error
		result ctrl.Result
	)

	log := log.FromContext(ctx).WithName("mpas-project-reconcile")
	log.Info("starting mpas-project reconcile loop")

	obj := &mpasv1alpha1.Project{}
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return result, nil
		}
		retErr = fmt.Errorf("failed to get project %s/%s: %w", req.NamespacedName.Namespace, req.NamespacedName.Name, err)
		return result, retErr
	}

	if obj.DeletionTimestamp != nil {
		log.Info("project is being deleted...")
		return result, nil
	}

	//Initialize the patch helper with the current version of the object.
	patchHelper := patch.NewSerialPatcher(obj, r.Client)

	// Always attempt to patch the object and status after each reconciliation.
	defer func() {
		// Patching has not been set up, or the controller errored earlier.
		if patchHelper == nil {
			return
		}

		if condition := conditions.Get(obj, meta.StalledCondition); condition != nil && condition.Status == metav1.ConditionTrue {
			conditions.Delete(obj, meta.ReconcilingCondition)
		}

		// Check if it's a successful reconciliation.
		// We don't set Requeue in case of error, so we can safely check for Requeue.
		if result.RequeueAfter == obj.GetRequeueAfter() && !result.Requeue && retErr == nil {
			// Remove the reconciling condition if it's set.
			conditions.Delete(obj, meta.ReconcilingCondition)

			// Set the return err as the ready failure message if the resource is not ready, but also not reconciling or stalled.
			if ready := conditions.Get(obj, meta.ReadyCondition); ready != nil && ready.Status == metav1.ConditionFalse && !conditions.IsStalled(obj) {
				retErr = errors.New(conditions.GetMessage(obj, meta.ReadyCondition))
			}
		}

		// If still reconciling then reconciliation did not succeed, set to ProgressingWithRetry to
		// indicate that reconciliation will be retried.
		if conditions.IsReconciling(obj) {
			reconciling := conditions.Get(obj, meta.ReconcilingCondition)
			reconciling.Reason = meta.ProgressingWithRetryReason
			conditions.Set(obj, reconciling)
		}

		// If not reconciling or stalled than mark Ready=True
		if !conditions.IsReconciling(obj) && !conditions.IsStalled(obj) &&
			retErr == nil &&
			result.RequeueAfter == obj.GetRequeueAfter() {
			conditions.MarkTrue(obj, meta.ReadyCondition, meta.SucceededReason, "Reconciliation success")
		}
		// Set status observed generation option if the component is stalled or ready.
		if conditions.IsStalled(obj) || conditions.IsReady(obj) {
			obj.Status.ObservedGeneration = obj.Generation
		}

		// Update the object.
		if err := patchHelper.Patch(ctx, obj); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()

	result, retErr = r.reconcile(ctx, obj, patchHelper)

	return ctrl.Result{}, nil
}

func (r *ProjectReconciler) reconcile(ctx context.Context, obj *mpasv1alpha1.Project, sp *patch.SerialPatcher) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	rreconcile.ProgressiveStatus(false, obj, meta.ProgressingReason, "reconciliation in progress")

	if obj.Generation != obj.Status.ObservedGeneration {
		rreconcile.ProgressiveStatus(false, obj, meta.ProgressingReason,
			"processing object: new generation %d -> %d", obj.Status.ObservedGeneration, obj.Generation)
		if err := sp.Patch(ctx, obj); err != nil {
			return ctrl.Result{}, err
		}
	}

	conditions.Delete(obj, meta.StalledCondition)

	ns, err := r.reconcileNamespace(ctx, obj)
	if err != nil {
		log.Error(err, "failed to create or update namespace")
		conditions.MarkStalled(obj, mpasv1alpha1.NamespaceCreateOrUpdateFailedReason, err.Error())
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.NamespaceCreateOrUpdateFailedReason, err.Error())
		return ctrl.Result{}, err
	}

	msg := fmt.Sprintf("Namespace %s is ready", ns.Name)
	conditions.MarkTrue(obj, mpasv1alpha1.NamespaceReadyCondition, meta.SucceededReason, msg)

	sa, err := r.reconcileServiceAccount(ctx, obj, ns.GetName())
	if err != nil {
		log.Error(err, "failed to create or update service account")
		conditions.MarkStalled(obj, mpasv1alpha1.ServiceAccountCreateOrUpdateFailedReason, err.Error())
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.ServiceAccountCreateOrUpdateFailedReason, err.Error())
		return ctrl.Result{}, err
	}

	msg = fmt.Sprintf("Service account %s/%s is ready", sa.Namespace, sa.Name)
	conditions.MarkTrue(obj, mpasv1alpha1.ServiceAccountReadyCondition, meta.SucceededReason, msg)

	if err := r.reconcileClusterRoleBinding(ctx, obj, sa); err != nil {
		log.Error(err, "failed to create or update cluster role binding")
		conditions.MarkStalled(obj, mpasv1alpha1.ClusterRoleBindingCreateOrUpdateFailedReason, err.Error())
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.ClusterRoleBindingCreateOrUpdateFailedReason, err.Error())
		return ctrl.Result{}, err
	}

	msg = fmt.Sprintf("Cluster role binding %s/%s is ready", obj.Name, obj.Name)
	conditions.MarkTrue(obj, mpasv1alpha1.RBACReadyCondition, meta.SucceededReason, msg)

	repo, err := r.reconcileRepository(ctx, obj)
	if err != nil {
		log.Error(err, "failed to create or update repository")
		conditions.MarkStalled(obj, mpasv1alpha1.RepositoryCreateOrUpdateFailedReason, err.Error())
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.RepositoryCreateOrUpdateFailedReason, err.Error())
		return ctrl.Result{}, err
	}

	gitSource, err := r.reconcileFluxGitRepository(ctx, obj, repo)
	if err != nil {
		log.Error(err, "failed to create or update flux git repository")
		conditions.MarkStalled(obj, mpasv1alpha1.FluxGitRepositoryCreateOrUpdateFailedReason, err.Error())
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.FluxGitRepositoryCreateOrUpdateFailedReason, err.Error())
		return ctrl.Result{}, err
	}

	if err := r.reconcileFluxKustomizations(ctx, obj, gitSource); err != nil {
		log.Error(err, "failed to create or update flux kustomizations")
		conditions.MarkStalled(obj, mpasv1alpha1.FluxKustomizationsCreateOrUpdateFailedReason, err.Error())
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.FluxKustomizationsCreateOrUpdateFailedReason, err.Error())
		return ctrl.Result{}, err
	}

	obj.Status.ObservedGeneration = obj.Generation

	// Remove any stale Ready condition, most likely False, set above. Its value
	// is derived from the overall result of the reconciliation in the deferred
	// block at the very end.
	conditions.Delete(obj, meta.ReadyCondition)

	return ctrl.Result{RequeueAfter: obj.GetRequeueAfter()}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mpasv1alpha1.Project{}).
		Complete(r)
}

func (r *ProjectReconciler) reconcileNamespace(ctx context.Context, obj *mpasv1alpha1.Project) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: obj.GetName(),
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
		if ns.ObjectMeta.CreationTimestamp.IsZero() {
			if err := controllerutil.SetOwnerReference(obj, ns, r.Scheme); err != nil {
				return fmt.Errorf("failed to set owner reference on namespace: %w", err)
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create or update namespace: %w", err)
	}

	return ns, nil
}

func (r *ProjectReconciler) reconcileServiceAccount(ctx context.Context, obj *mpasv1alpha1.Project, namespace string) (*corev1.ServiceAccount, error) {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      obj.GetName(),
			Namespace: obj.GetName(),
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		if sa.ObjectMeta.CreationTimestamp.IsZero() {
			if err := controllerutil.SetOwnerReference(obj, sa, r.Scheme); err != nil {
				return fmt.Errorf("failed to set owner reference on service account: %w", err)
			}
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create or update service account: %w", err)
	}

	return sa, nil
}

func (r *ProjectReconciler) reconcileClusterRoleBinding(ctx context.Context, obj *mpasv1alpha1.Project, sa *corev1.ServiceAccount) error {
	// TODO(@jmickey): Confirm the name of the ClusterRole to bind Project ServiceAccounts.
	key := types.NamespacedName{
		Name:      "mpas-projects-clusterrole", // - Verify this
		Namespace: obj.GetNamespace(),
	}

	cr := &rbacv1.ClusterRole{}
	if err := r.Client.Get(ctx, key, cr); err != nil {
		return fmt.Errorf("failed to get projects cluster role: %w", err)
	}

	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      obj.GetName(),
			Namespace: obj.GetName(),
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      sa.GetName(),
				Namespace: sa.GetNamespace(),
			},
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     cr.GetName(),
			APIGroup: "rbac.authorization.k8s.io",
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, crb, func() error {
		if crb.ObjectMeta.CreationTimestamp.IsZero() {
			if err := controllerutil.SetOwnerReference(obj, crb, r.Scheme); err != nil {
				return fmt.Errorf("failed to set owner reference on cluster role binding: %w", err)
			}
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("failed to create or update cluster role binding: %w", err)
	}

	return nil
}

func (r *ProjectReconciler) reconcileRepository(ctx context.Context, obj *mpasv1alpha1.Project) (*gcv1alpha1.Repository, error) {
	if err := gcv1alpha1.AddToScheme(r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to add gcv1alpha1 to scheme: %w", err)
	}

	repo := &gcv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      obj.GetName(),
			Namespace: obj.GetName(),
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, repo, func() error {
		if repo.ObjectMeta.CreationTimestamp.IsZero() {
			if err := controllerutil.SetOwnerReference(obj, repo, r.Scheme); err != nil {
				return fmt.Errorf("failed to set owner reference on repository: %w", err)
			}
		}
		// obj.Spec.Git matches the Repository spec, so we can just assign it.
		repo.Spec = obj.Spec.Git
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create or update repository: %w", err)
	}

	return repo, nil
}

func (r *ProjectReconciler) reconcileFluxGitRepository(ctx context.Context, obj *mpasv1alpha1.Project, repo *gcv1alpha1.Repository) (*sourcev1.GitRepository, error) {
	if err := sourcev1.AddToScheme(r.Scheme); err != nil {
		return nil, fmt.Errorf("failed to add sourcev1 to scheme: %w", err)
	}

	gitRepo := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      obj.GetName(),
			Namespace: obj.GetName(),
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, gitRepo, func() error {
		if repo.ObjectMeta.CreationTimestamp.IsZero() {
			if err := controllerutil.SetOwnerReference(obj, repo, r.Scheme); err != nil {
				return fmt.Errorf("failed to set owner reference on repository: %w", err)
			}
		}

		gitRepo.Spec.URL = repo.GetRepositoryURL()
		gitRepo.Spec.Reference = &sourcev1.GitRepositoryRef{
			Branch: repo.Spec.DefaultBranch,
		}
		gitRepo.Spec.SecretRef = (*meta.LocalObjectReference)(&repo.Spec.Credentials.SecretRef)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create or update repository: %w", err)
	}

	return gitRepo, nil
}

func (r *ProjectReconciler) reconcileFluxKustomizations(ctx context.Context, obj *mpasv1alpha1.Project, gitSource *sourcev1.GitRepository) error {
	if err := kustomizev1.AddToScheme(r.Scheme); err != nil {
		return fmt.Errorf("failed to add kustomizev1 to scheme: %w", err)
	}

	paths := []string{"subscriptions", "targets", "products", "generators"}

	for _, path := range paths {
		name := fmt.Sprintf("%s-%s", obj.GetName(), path)
		kustomization := &kustomizev1.Kustomization{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: obj.GetName(),
			},
		}

		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, kustomization, func() error {
			if kustomization.ObjectMeta.CreationTimestamp.IsZero() {
				if err := controllerutil.SetOwnerReference(obj, kustomization, r.Scheme); err != nil {
					return fmt.Errorf("failed to set owner reference on kustomization: %w", err)
				}
			}

			kustomization.Spec.Path = path
			kustomization.Spec.Interval = metav1.Duration{
				Duration: 5 * time.Minute,
			}
			kustomization.Spec.SourceRef = kustomizev1.CrossNamespaceSourceReference{
				Kind:      gitSource.Kind,
				Name:      gitSource.Name,
				Namespace: gitSource.Namespace,
			}

			return nil
		})

		if err != nil {
			return fmt.Errorf("failed to create or update kustomization: %w", err)
		}
	}

	return nil
}
