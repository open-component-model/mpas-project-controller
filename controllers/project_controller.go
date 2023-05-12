// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"errors"
	"fmt"

	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1beta2"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/fluxcd/pkg/runtime/patch"
	rreconcile "github.com/fluxcd/pkg/runtime/reconcile"
	sourcev1 "github.com/fluxcd/source-controller/api/v1beta2"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	gcv1alpha1 "github.com/open-component-model/git-controller/apis/mpas/v1alpha1"
	mpasv1alpha1 "github.com/open-component-model/mpas-project-controller/api/v1alpha1"
	"github.com/open-component-model/mpas-project-controller/inventory"
)

const (
	SystemNamespace = "mpas-system"
)

// ProjectReconciler reconciles a Project object
type ProjectReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	ClusterRoleName       string
	Prefix                string
	DefaultCommitTemplate CommitTemplate
}

// CommitTemplate defines the default commit template for a project if one is not provided in the spec.
type CommitTemplate struct {
	Name    string
	Email   string
	Message string
}

//+kubebuilder:rbac:groups="",resources=namespaces;serviceaccounts;secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch
//+kubebuilder:rbac:groups=mpas.ocm.software,resources=projects;targets;repositories;productdeployments;productdeploymentgenerators;productdeploymentpipelines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mpas.ocm.software,resources=subscriptions,verbs=get;list;watch
//+kubebuilder:rbac:groups=delivery.ocm.software,resources=componentsubscriptions;componentversions;configurations;localizations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=source.toolkit.fluxcd.io;kustomize.toolkit.fluxcd.io,resources=gitrepositories;ocirepositories;kustomizations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mpas.ocm.software,resources=projects/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mpas.ocm.software,resources=projects/finalizers,verbs=update

// SetupWithManager sets up the controller with the Manager.
func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mpasv1alpha1.Project{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, retErr error) {
	logger := log.FromContext(ctx).WithName("mpas-project-reconcile")
	logger.Info("starting mpas-project reconcile loop")

	obj := &mpasv1alpha1.Project{}
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return
		}
		return ctrl.Result{}, fmt.Errorf("failed to get project %s/%s: %w", req.NamespacedName.Namespace, req.NamespacedName.Name, err)
	}
	//Initialize the patch helper with the current version of the object.
	patchHelper := patch.NewSerialPatcher(obj, r.Client)

	defer func() {
		if err := r.finalizeStatus(ctx, obj, patchHelper); err != nil {
			errors.Join(retErr, err)
		}
	}()

	if !controllerutil.ContainsFinalizer(obj, mpasv1alpha1.ProjectFinalizer) {
		controllerutil.AddFinalizer(obj, mpasv1alpha1.ProjectFinalizer)
		return ctrl.Result{Requeue: true}, nil
	}

	if obj.DeletionTimestamp != nil {
		logger.Info("project is being deleted...")
		return r.finalize(ctx, obj)
	}

	return r.reconcile(ctx, obj, patchHelper)
}

func (r *ProjectReconciler) reconcile(ctx context.Context, obj *mpasv1alpha1.Project, sp *patch.SerialPatcher) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	shouldRequeue := true
	if conditions.Has(obj, meta.ReconcilingCondition) &&
		conditions.GetReason(obj, meta.ReconcilingCondition) == mpasv1alpha1.WaitingOnResourcesReason {
		shouldRequeue = false
	}

	rreconcile.ProgressiveStatus(false, obj, meta.ProgressingReason, "reconciliation in progress")
	if err := r.patch(ctx, obj, sp); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to patch object: %w", err)
	}

	if obj.Generation != obj.Status.ObservedGeneration {
		conditions.MarkUnknown(obj, meta.ReadyCondition, meta.ProgressingReason, "reconciliation in progress")
		conditions.MarkReconciling(obj, meta.ProgressingReason, "processing object: new generation %d -> %d", obj.Status.ObservedGeneration, obj.Generation)
		if err := r.patch(ctx, obj, sp); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to patch object: %w", err)
		}
	}

	conditions.Delete(obj, meta.StalledCondition)

	oldInventory := inventory.New()
	if obj.Status.Inventory != nil {
		obj.Status.Inventory.DeepCopyInto(oldInventory)
	}

	ns, err := r.reconcileNamespace(ctx, obj)
	if err != nil {
		logger.Error(err, "failed to reconcile namespace")
		conditions.MarkStalled(obj, mpasv1alpha1.NamespaceCreateOrUpdateFailedReason, err.Error())
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.NamespaceCreateOrUpdateFailedReason, err.Error())
		return ctrl.Result{}, fmt.Errorf("error reconciling namespace: %w", err)
	}

	sa, err := r.reconcileServiceAccount(ctx, obj, ns.GetName())
	if err != nil {
		logger.Error(err, "failed to reconcile service account")
		conditions.MarkStalled(obj, mpasv1alpha1.ServiceAccountCreateOrUpdateFailedReason, err.Error())
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.ServiceAccountCreateOrUpdateFailedReason, err.Error())
		return ctrl.Result{}, fmt.Errorf("error reconciling service account: %w", err)
	}

	role, err := r.reconcileRole(ctx, obj)
	if err != nil {
		logger.Error(err, "failed to reconcile role")
		conditions.MarkStalled(obj, mpasv1alpha1.RBACCreateOrUpdateFailedReason, err.Error())
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.RBACCreateOrUpdateFailedReason, err.Error())
		return ctrl.Result{}, fmt.Errorf("error reconciling project namespace role: %w", err)
	}

	roleBindings, err := r.reconcileRoleBindings(ctx, obj, sa)
	if err != nil {
		logger.Error(err, "failed to reconcile cluster role binding")
		conditions.MarkStalled(obj, mpasv1alpha1.RBACCreateOrUpdateFailedReason, err.Error())
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.RBACCreateOrUpdateFailedReason, err.Error())
		return ctrl.Result{}, fmt.Errorf("error reconciling role bindings: %w", err)
	}

	repo, err := r.reconcileRepository(ctx, obj)
	if err != nil {
		logger.Error(err, "failed to reconcile repository")
		conditions.MarkStalled(obj, mpasv1alpha1.RepositoryCreateOrUpdateFailedReason, err.Error())
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.RepositoryCreateOrUpdateFailedReason, err.Error())
		return ctrl.Result{}, fmt.Errorf("error reconciling repository: %w", err)
	}

	obj.Status.RepositoryRef = &mpasv1alpha1.RepositoryRef{
		Name:      repo.GetName(),
		Namespace: repo.GetNamespace(),
	}

	gitRepo, err := r.reconcileFluxGitRepository(ctx, obj, repo)
	if err != nil {
		logger.Error(err, "failed to reconcile flux git repository")
		conditions.MarkStalled(obj, mpasv1alpha1.FluxGitRepositoryCreateOrUpdateFailedReason, err.Error())
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.FluxGitRepositoryCreateOrUpdateFailedReason, err.Error())
		return ctrl.Result{}, fmt.Errorf("error reconciling flux git source: %w", err)
	}

	kustomizations, err := r.reconcileFluxKustomizations(ctx, obj)
	if err != nil {
		logger.Error(err, "failed to reconcile flux kustomizations")
		conditions.MarkStalled(obj, mpasv1alpha1.FluxKustomizationsCreateOrUpdateFailedReason, err.Error())
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.FluxKustomizationsCreateOrUpdateFailedReason, err.Error())
		return ctrl.Result{}, fmt.Errorf("error reconciling flux kustomizations: %w", err)
	}

	// Requeue the project only if it was previously not requeued in order to wait for resources to be created.
	// If we don't do this, then the resources created above will not be correctly populated when adding to the inventory.
	if shouldRequeue {
		logger.Info("waiting for resources to be ready")
		conditions.MarkUnknown(obj, meta.ReadyCondition, meta.ProgressingReason, "reconciliation in progress")
		conditions.MarkReconciling(obj, mpasv1alpha1.WaitingOnResourcesReason, "waiting for resources to be ready")
		if err := r.patch(ctx, obj, sp); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to patch object: %w", err)
		}
		return ctrl.Result{Requeue: true}, nil
	}

	newInventory := inventory.New()
	if err := inventory.Add(newInventory, ns, sa, role, repo, gitRepo); err != nil {
		logger.Error(err, "failed to add resources to inventory")
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.ReconciliationFailedReason, err.Error())
		return ctrl.Result{}, fmt.Errorf("error adding resources to inventory: %w", err)
	}
	for _, roleBinding := range roleBindings {
		if err := inventory.Add(newInventory, roleBinding); err != nil {
			logger.Error(err, "failed to add resources to inventory")
			conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.ReconciliationFailedReason, err.Error())
			return ctrl.Result{}, fmt.Errorf("error adding resources to inventory: %w", err)
		}
	}
	for _, kustomization := range kustomizations {
		if err := inventory.Add(newInventory, kustomization); err != nil {
			logger.Error(err, "failed to add resources to inventory")
			conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.ReconciliationFailedReason, err.Error())
			return ctrl.Result{}, fmt.Errorf("error adding resources to inventory: %w", err)
		}
	}
	obj.Status.Inventory = newInventory
	obj.Status.ObservedGeneration = obj.Generation

	logger.Info("getting stale objects")
	// If the inventory has changed, then we need to prune.
	staleObjects, err := inventory.Diff(oldInventory, newInventory)
	if err != nil {
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.ReconciliationFailedReason, err.Error())
		return ctrl.Result{}, fmt.Errorf("error generating inventory diff inventory: %w", err)
	}

	logger.Info("deleting stale objects if any exist")
	// Garbage collect stale objects, as long as prune it set to true.
	if err := r.prune(ctx, obj, staleObjects); err != nil {
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.ReconciliationFailedReason, err.Error())
		return ctrl.Result{}, fmt.Errorf("error pruning stale objects: %w", err)
	}

	// Resource is ready
	logger.Info("resource is ready")
	conditions.MarkTrue(obj, meta.ReadyCondition, meta.SucceededReason, "Reconciliation success")

	return ctrl.Result{RequeueAfter: obj.GetRequeueAfter()}, nil
}

func (r *ProjectReconciler) reconcileNamespace(ctx context.Context, obj *mpasv1alpha1.Project) (*corev1.Namespace, error) {
	name := obj.GetNameWithPrefix(r.Prefix)
	ns := &corev1.Namespace{}

	if err := r.Client.Get(ctx, types.NamespacedName{Name: name}, ns); err != nil {
		if apierrors.IsNotFound(err) {
			ns.Name = name
			if err := r.Client.Create(ctx, ns); err != nil {
				return nil, fmt.Errorf("failed to create namespace: %w", err)
			}

			_ = r.Client.Get(ctx, types.NamespacedName{Name: name}, ns)

			return ns, nil
		}

		return nil, fmt.Errorf("failed to get namespace: %w", err)
	}

	return ns, nil
}

func (r *ProjectReconciler) reconcileServiceAccount(ctx context.Context, obj *mpasv1alpha1.Project, namespace string) (*corev1.ServiceAccount, error) {
	name := obj.GetNameWithPrefix(r.Prefix)
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create or update service account: %w", err)
	}

	_ = r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: name}, sa)

	return sa, nil
}

func (r *ProjectReconciler) reconcileRole(ctx context.Context, obj *mpasv1alpha1.Project) (*rbacv1.Role, error) {
	name := obj.GetNameWithPrefix(r.Prefix)
	role := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, role, func() error {
		role.Rules = []rbacv1.PolicyRule{
			{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"mpas.ocm.software"},
				Resources: []string{
					"repositories",
					"targets",
					"productdeployments",
					"productdeploymentgenerators",
					"productdeploymentpipelines",
				},
				Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"delivery.ocm.software"},
				Resources: []string{"componentsubscriptions", "componentversions", "localizations", "configurations"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{"source.toolkit.fluxcd.io", "kustomize.toolkit.fluxcd.io"},
				Resources: []string{"ocirepositories", "kustomizations"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create or update role: %w", err)
	}

	_ = r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: name}, role)

	return role, nil
}

func (r *ProjectReconciler) reconcileRoleBindings(ctx context.Context, obj *mpasv1alpha1.Project, sa *corev1.ServiceAccount) ([]*rbacv1.RoleBinding, error) {
	name := obj.GetNameWithPrefix(r.Prefix)
	key := types.NamespacedName{
		Name: r.ClusterRoleName,
	}

	cr := &rbacv1.ClusterRole{}
	if err := r.Client.Get(ctx, key, cr); err != nil {
		return nil, fmt.Errorf("failed to get projects cluster role: %w", err)
	}

	mpasRoleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: SystemNamespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, mpasRoleBinding, func() error {
		mpasRoleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      sa.GetName(),
				Namespace: sa.GetNamespace(),
			},
		}

		mpasRoleBinding.RoleRef = rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     cr.GetName(),
			APIGroup: "rbac.authorization.k8s.io",
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create or update role binding: %w", err)
	}

	projectRoleBindingCR := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name + "-clusterrole",
			Namespace: name,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, projectRoleBindingCR, func() error {
		projectRoleBindingCR.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      sa.GetName(),
				Namespace: sa.GetNamespace(),
			},
		}

		projectRoleBindingCR.RoleRef = rbacv1.RoleRef{
			Kind:     "ClusterRole",
			Name:     cr.GetName(),
			APIGroup: "rbac.authorization.k8s.io",
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create or update role binding: %w", err)
	}

	projectRoleBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, projectRoleBinding, func() error {
		projectRoleBinding.Subjects = []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      sa.GetName(),
				Namespace: sa.GetNamespace(),
			},
		}

		projectRoleBinding.RoleRef = rbacv1.RoleRef{
			Kind:     "Role",
			Name:     name,
			APIGroup: "rbac.authorization.k8s.io",
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create or update role binding: %w", err)
	}

	_ = r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: SystemNamespace}, mpasRoleBinding)
	_ = r.Client.Get(ctx, types.NamespacedName{Name: name + "-clusterrole", Namespace: name}, projectRoleBindingCR)
	_ = r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: name}, projectRoleBinding)

	return []*rbacv1.RoleBinding{mpasRoleBinding, projectRoleBindingCR, projectRoleBinding}, nil
}

func (r *ProjectReconciler) reconcileRepository(ctx context.Context, obj *mpasv1alpha1.Project) (*gcv1alpha1.Repository, error) {
	name := obj.GetNameWithPrefix(r.Prefix)
	repo := &gcv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: SystemNamespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, repo, func() error {
		// obj.Spec.Git matches the Repository spec, so we can just assign it.
		repo.Spec = obj.Spec.Git

		if repo.Spec.CommitTemplate == nil {
			repo.Spec.CommitTemplate = &gcv1alpha1.CommitTemplate{
				Name:    r.DefaultCommitTemplate.Name,
				Email:   r.DefaultCommitTemplate.Email,
				Message: r.DefaultCommitTemplate.Message,
			}
		}

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create or update repository: %w", err)
	}

	_ = r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: SystemNamespace}, repo)

	return repo, nil
}

func (r *ProjectReconciler) reconcileFluxGitRepository(ctx context.Context, obj *mpasv1alpha1.Project, repo *gcv1alpha1.Repository) (*sourcev1.GitRepository, error) {
	name := obj.GetNameWithPrefix(r.Prefix)
	gitRepo := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: SystemNamespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, gitRepo, func() error {
		gitRepo.Spec.URL = repo.GetRepositoryURL()
		gitRepo.Spec.Reference = &sourcev1.GitRepositoryRef{
			Branch: repo.Spec.DefaultBranch,
		}
		gitRepo.Spec.SecretRef = (*meta.LocalObjectReference)(&repo.Spec.Credentials.SecretRef)
		gitRepo.Spec.Interval = obj.Spec.Flux.Interval

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create or update repository: %w", err)
	}

	_ = r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: SystemNamespace}, gitRepo)

	return gitRepo, nil
}

func (r *ProjectReconciler) reconcileFluxKustomizations(ctx context.Context, obj *mpasv1alpha1.Project) ([]*kustomizev1.Kustomization, error) {
	prefixedName := obj.GetNameWithPrefix(r.Prefix)
	paths := []string{"subscriptions", "targets", "products", "generators"}
	kustomizations := make([]*kustomizev1.Kustomization, 0)

	for _, path := range paths {
		name := fmt.Sprintf("%s-%s", prefixedName, path)
		kustomization := &kustomizev1.Kustomization{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: SystemNamespace,
			},
		}

		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, kustomization, func() error {
			kustomization.Spec.Path = path
			kustomization.Spec.Interval = obj.Spec.Flux.Interval
			kustomization.Spec.SourceRef = kustomizev1.CrossNamespaceSourceReference{
				Kind:      "GitRepository",
				Name:      prefixedName,
				Namespace: obj.GetNamespace(),
			}

			return nil
		})

		if err != nil {
			return nil, fmt.Errorf("failed to create or update kustomization: %w", err)
		}

		_ = r.Client.Get(ctx, types.NamespacedName{Name: name, Namespace: SystemNamespace}, kustomization)

		kustomizations = append(kustomizations, kustomization)
	}

	return kustomizations, nil
}

func (r *ProjectReconciler) finalize(ctx context.Context, obj *mpasv1alpha1.Project) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	var retErr error
	if obj.Spec.Prune &&
		obj.Status.Inventory != nil &&
		obj.Status.Inventory.Entries != nil {
		objects, _ := inventory.List(obj.Status.Inventory)

		for _, object := range objects {
			existingObj := object.DeepCopy()
			if err := r.Client.Get(ctx, client.ObjectKeyFromObject(object), existingObj); err != nil {
				if !apierrors.IsNotFound(err) {
					logger.Error(err, "failed to get object for deletion")
					errors.Join(retErr, err)
				}
			}

			if err := r.Client.Delete(ctx, object); err != nil {
				logger.Error(err, "failed to delete object", "object", object)
				errors.Join(retErr, err)
			}
		}

		if retErr != nil {
			return ctrl.Result{}, retErr
		}
	}

	// Remove our finalizer from the list and update it
	controllerutil.RemoveFinalizer(obj, mpasv1alpha1.ProjectFinalizer)
	return ctrl.Result{}, nil
}

func (r *ProjectReconciler) prune(ctx context.Context, obj *mpasv1alpha1.Project, staleObjects []*unstructured.Unstructured) error {
	logger := log.FromContext(ctx)
	var retErr error

	if !obj.Spec.Prune {
		return nil
	}

	for _, object := range staleObjects {
		existingObj := object.DeepCopy()
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(object), existingObj); err != nil {
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to get object for deletion")
				errors.Join(retErr, err)
			}
		}

		if err := r.Client.Delete(ctx, object); err != nil {
			logger.Error(err, "failed to delete object", "object", object)
			errors.Join(retErr, err)
		}
	}

	return retErr
}

func (r *ProjectReconciler) finalizeStatus(ctx context.Context, obj *mpasv1alpha1.Project, patcher *patch.SerialPatcher) error {
	// Remove the Reconciling condition and update the observed generation
	// if the reconciliation was successful.
	if conditions.IsTrue(obj, meta.ReadyCondition) {
		conditions.Delete(obj, meta.ReconcilingCondition)
		obj.Status.ObservedGeneration = obj.Generation
	}

	// Set the Reconciling reason to ProgressingWithRetry if the
	// reconciliation has failed.
	if conditions.IsFalse(obj, meta.ReadyCondition) &&
		conditions.Has(obj, meta.ReconcilingCondition) &&
		conditions.GetReason(obj, meta.ReconcilingCondition) != mpasv1alpha1.WaitingOnResourcesReason {
		rc := conditions.Get(obj, meta.ReconcilingCondition)
		rc.Reason = meta.ProgressingWithRetryReason
		conditions.Set(obj, rc)
	}

	// Patch finalizers, status and conditions.
	return r.patch(ctx, obj, patcher)
}

func (r *ProjectReconciler) patch(ctx context.Context, obj *mpasv1alpha1.Project, patcher *patch.SerialPatcher) error {
	opts := []patch.Option{}
	ownedConditions := []string{
		meta.ReadyCondition,
		meta.ReconcilingCondition,
		meta.StalledCondition,
	}
	opts = append(opts,
		patch.WithOwnedConditions{Conditions: ownedConditions},
		patch.WithForceOverwriteConditions{},
	)

	if err := patcher.Patch(ctx, obj, opts...); err != nil {
		return fmt.Errorf("failed to patch object: %w", err)
	}

	return nil
}
