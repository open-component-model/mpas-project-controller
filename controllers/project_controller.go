// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	_ "embed" // embedding
	"errors"
	"fmt"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	v1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	kustomizev1 "github.com/fluxcd/kustomize-controller/api/v1"
	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/fluxcd/pkg/runtime/patch"
	rreconcile "github.com/fluxcd/pkg/runtime/reconcile"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	gcv1alpha1 "github.com/open-component-model/git-controller/apis/mpas/v1alpha1"
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

	mpasv1alpha1 "github.com/open-component-model/mpas-project-controller/api/v1alpha1"
	"github.com/open-component-model/mpas-project-controller/inventory"
)

const (
	ControllerName = "mpas-project-controller"
)

const (
	// Mandatory Labels.
	labelComponent = "app.kubernetes.io/component"
	labelCreatedBy = "app.kubernetes.io/created-by"
	labelManagedBy = "app.kubernetes.io/managed-by"
	labelInstance  = "app.kubernetes.io/instance"
	labelName      = "app.kubernetes.io/name"
	labelPartOf    = "app.kubernetes.io/part-of"
)

// ProjectReconciler reconciles a Project object.
type ProjectReconciler struct {
	client.Client
	Scheme                *runtime.Scheme
	ClusterRoleName       string
	Prefix                string
	DefaultCommitTemplate mpasv1alpha1.CommitTemplate
	DefaultNamespace      string
	IssuerName            string
	RegistryAddr          string
}

//+kubebuilder:rbac:groups="",resources=namespaces;serviceaccounts;secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=get;list;watch
//nolint:lll // rbac comment
//+kubebuilder:rbac:groups=mpas.ocm.software,resources=projects;targets;repositories;productdeployments;productdeploymentgenerators;productdeploymentpipelines,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mpas.ocm.software,resources=subscriptions,verbs=get;list;watch
//nolint:lll // rbac comment
//+kubebuilder:rbac:groups=delivery.ocm.software,resources=componentsubscriptions;componentversions;configurations;localizations,verbs=get;list;watch;create;update;patch;delete
//nolint:lll // rbac comment
//+kubebuilder:rbac:groups=source.toolkit.fluxcd.io;kustomize.toolkit.fluxcd.io,resources=gitrepositories;ocirepositories;kustomizations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=mpas.ocm.software,resources=projects/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=mpas.ocm.software,resources=projects/finalizers,verbs=update
//+kubebuilder:rbac:groups=cert-manager.io,resources=certificates,verbs=create;update;get;list;delete;watch
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch

// SetupWithManager sets up the controller with the Manager.
func (r *ProjectReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mpasv1alpha1.Project{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.Namespace{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&gcv1alpha1.Repository{}).
		Owns(&sourcev1.GitRepository{}).
		Owns(&kustomizev1.Kustomization{}).
		Complete(r)
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ProjectReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, retErr error) {
	logger := log.FromContext(ctx).WithName("mpas-project-reconcile")
	logger.Info("starting mpas-project reconcile loop")

	obj := &mpasv1alpha1.Project{}
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to get project %s/%s: %w", req.NamespacedName.Namespace, req.NamespacedName.Name, err)
	}
	// Initialize the patch helper with the current version of the object.
	patchHelper := patch.NewSerialPatcher(obj, r.Client)

	defer func() {
		if err := r.finalizeStatus(ctx, obj, patchHelper); err != nil {
			retErr = errors.Join(retErr, err)
		}
	}()

	if !controllerutil.ContainsFinalizer(obj, mpasv1alpha1.ProjectFinalizer) {
		controllerutil.AddFinalizer(obj, mpasv1alpha1.ProjectFinalizer)

		return ctrl.Result{Requeue: true}, nil
	}

	if obj.DeletionTimestamp != nil {
		logger.Info("project is being deleted...")

		return ctrl.Result{}, r.finalize(ctx, obj)
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
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}

		return ctrl.Result{}, fmt.Errorf("failed to patch object: %w", err)
	}

	if obj.Generation != obj.Status.ObservedGeneration {
		conditions.MarkUnknown(obj, meta.ReadyCondition, meta.ProgressingReason, "reconciliation in progress")
		conditions.MarkReconciling(obj, meta.ProgressingReason, "processing object: new generation %d -> %d", obj.Status.ObservedGeneration, obj.Generation)
		if err := r.patch(ctx, obj, sp); err != nil {
			if apierrors.IsNotFound(err) {
				return ctrl.Result{}, nil
			}

			return ctrl.Result{}, fmt.Errorf("failed to patch object: %w", err)
		}
	}

	conditions.Delete(obj, meta.StalledCondition)

	oldInventory := inventory.New()
	if obj.Status.Inventory != nil {
		obj.Status.Inventory.DeepCopyInto(oldInventory)
	}

	objects, err := r.reconcileInventory(ctx, obj)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Requeue the project only if it was previously not requeued in order to wait for resources to be created.
	// If we don't do this, then the resources created above will not be correctly populated when adding to the inventory.
	if shouldRequeue {
		logger.Info("waiting for resources to be ready")
		conditions.MarkUnknown(obj, meta.ReadyCondition, meta.ProgressingReason, "reconciliation in progress")
		conditions.MarkReconciling(obj, mpasv1alpha1.WaitingOnResourcesReason, "waiting for resources to be ready")
		if err := r.patch(ctx, obj, sp); err != nil {
			if apierrors.IsNotFound(err) {
				return ctrl.Result{}, nil
			}

			return ctrl.Result{}, fmt.Errorf("failed to patch object: %w", err)
		}

		return ctrl.Result{Requeue: true}, nil
	}

	newInventory := inventory.New()
	if err := inventory.Add(newInventory, objects...); err != nil {
		conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.ReconciliationFailedReason, err.Error())

		return ctrl.Result{}, fmt.Errorf("error adding resources to inventory: %w", err)
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

func (r *ProjectReconciler) markStalled(reason string, obj *mpasv1alpha1.Project, err error) {
	conditions.MarkStalled(obj, reason, err.Error())
	conditions.MarkFalse(obj, meta.ReadyCondition, reason, err.Error())
}

func (r *ProjectReconciler) reconcileNamespace(ctx context.Context, obj *mpasv1alpha1.Project) (*corev1.Namespace, error) {
	name := obj.GetNameWithPrefix(r.Prefix)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, ns, func() error {
		if ns.Labels == nil {
			ns.Labels = make(map[string]string)
		}

		if _, ok := ns.Labels[mpasv1alpha1.ProjectKey]; !ok {
			ns.Labels[mpasv1alpha1.ProjectKey] = obj.Name
		}

		r.applyMandatoryLabels("namespace", "namespace", "namespace", ns.Labels)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create namespace: %w", err)
	}

	return ns, nil
}

func (r *ProjectReconciler) reconcileServiceAccount(ctx context.Context, obj *mpasv1alpha1.Project) (*corev1.ServiceAccount, error) {
	name := obj.GetNameWithPrefix(r.Prefix)
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: name,
		},
	}

	// Get the service account, if it doesn't exist, create it,

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, sa, func() error {
		if sa.Labels == nil {
			sa.Labels = make(map[string]string)
		}

		r.applyMandatoryLabels("serviceaccount", "rbac", "serviceaccount", sa.Labels)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create or update service account: %w", err)
	}

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

		if role.Labels == nil {
			role.Labels = make(map[string]string)
		}

		r.applyMandatoryLabels("role", "rbac", "role", role.Labels)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create or update role: %w", err)
	}

	return role, nil
}

func (r *ProjectReconciler) reconcileRoleBindings(
	ctx context.Context,
	obj *mpasv1alpha1.Project,
	sa *corev1.ServiceAccount,
) ([]*rbacv1.RoleBinding, error) {
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
			Namespace: r.DefaultNamespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, mpasRoleBinding, func() error {
		if obj.GetNamespace() == r.DefaultNamespace && mpasRoleBinding.ObjectMeta.CreationTimestamp.IsZero() {
			if err := controllerutil.SetOwnerReference(obj, mpasRoleBinding, r.Scheme); err != nil {
				return fmt.Errorf("failed to set owner reference on namespace %s with error: %w", r.DefaultNamespace, err)
			}
		}

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

		if mpasRoleBinding.Labels == nil {
			mpasRoleBinding.Labels = make(map[string]string)
		}
		r.applyMandatoryLabels("clusterrole", "rbac", "clusterrole", mpasRoleBinding.Labels)

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

		if projectRoleBindingCR.Labels == nil {
			projectRoleBindingCR.Labels = make(map[string]string)
		}
		r.applyMandatoryLabels("clusterrole", "rbac", "clusterrole", projectRoleBindingCR.Labels)

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

		if projectRoleBinding.Labels == nil {
			projectRoleBinding.Labels = make(map[string]string)
		}
		r.applyMandatoryLabels("role", "rbac", "role", projectRoleBinding.Labels)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create or update role binding: %w", err)
	}

	return []*rbacv1.RoleBinding{mpasRoleBinding, projectRoleBindingCR, projectRoleBinding}, nil
}

func (r *ProjectReconciler) reconcileRepository(ctx context.Context, obj *mpasv1alpha1.Project) (*gcv1alpha1.Repository, error) {
	name := obj.GetNameWithPrefix(r.Prefix)
	repo := &gcv1alpha1.Repository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: r.DefaultNamespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, repo, func() error {
		if obj.GetNamespace() == r.DefaultNamespace && repo.ObjectMeta.CreationTimestamp.IsZero() {
			if err := controllerutil.SetOwnerReference(obj, repo, r.Scheme); err != nil {
				return fmt.Errorf("failed to set owner reference on namespace: %w", err)
			}
		}

		// obj.Spec.Git matches the Repository spec, so we can just assign it.
		repo.Spec = obj.Spec.Git

		if repo.Spec.CommitTemplate == nil {
			repo.Spec.CommitTemplate = &gcv1alpha1.CommitTemplate{
				Name:    r.DefaultCommitTemplate.Name,
				Email:   r.DefaultCommitTemplate.Email,
				Message: r.DefaultCommitTemplate.Message,
			}
		}

		if repo.Labels == nil {
			repo.Labels = make(map[string]string)
		}
		r.applyMandatoryLabels("repository", "manager", "repository", repo.Labels)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create or update repository: %w", err)
	}

	return repo, nil
}

func (r *ProjectReconciler) reconcileFluxGitRepository(
	ctx context.Context,
	obj *mpasv1alpha1.Project,
	repo *gcv1alpha1.Repository,
) (*sourcev1.GitRepository, error) {
	name := obj.GetNameWithPrefix(r.Prefix)

	gitRepo := &sourcev1.GitRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: r.DefaultNamespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, gitRepo, func() error {
		if obj.GetNamespace() == r.DefaultNamespace && gitRepo.ObjectMeta.CreationTimestamp.IsZero() {
			if err := controllerutil.SetOwnerReference(obj, gitRepo, r.Scheme); err != nil {
				return fmt.Errorf("failed to set owner reference on namespace: %w", err)
			}
		}

		gitRepo.Spec.URL = repo.GetRepositoryURL()
		gitRepo.Spec.Reference = &sourcev1.GitRepositoryRef{
			Branch: repo.Spec.DefaultBranch,
		}
		gitRepo.Spec.SecretRef = (*meta.LocalObjectReference)(&repo.Spec.Credentials.SecretRef)
		gitRepo.Spec.Interval = obj.Spec.Flux.Interval

		if gitRepo.Labels == nil {
			gitRepo.Labels = make(map[string]string)
		}
		r.applyMandatoryLabels("gitrepository", "manager", "gitrepository", gitRepo.Labels)

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create or update repository: %w", err)
	}

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
				Namespace: r.DefaultNamespace,
			},
		}

		_, err := controllerutil.CreateOrUpdate(ctx, r.Client, kustomization, func() error {
			if obj.GetNamespace() == r.DefaultNamespace && kustomization.ObjectMeta.CreationTimestamp.IsZero() {
				if err := controllerutil.SetOwnerReference(obj, kustomization, r.Scheme); err != nil {
					return fmt.Errorf("failed to set owner reference on namespace: %w", err)
				}
			}

			kustomization.Spec.Path = path
			kustomization.Spec.Interval = obj.Spec.Flux.Interval
			kustomization.Spec.SourceRef = kustomizev1.CrossNamespaceSourceReference{
				Kind:      "GitRepository",
				Name:      prefixedName,
				Namespace: obj.GetNamespace(),
			}
			kustomization.Spec.ServiceAccountName = ControllerName
			kustomization.Spec.TargetNamespace = prefixedName

			if kustomization.Labels == nil {
				kustomization.Labels = make(map[string]string)
			}
			r.applyMandatoryLabels("kustomization", "manager", "kustomization", kustomization.Labels)

			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create or update kustomization: %w", err)
		}

		kustomizations = append(kustomizations, kustomization)
	}

	return kustomizations, nil
}

func (r *ProjectReconciler) finalize(ctx context.Context, obj *mpasv1alpha1.Project) error {
	logger := log.FromContext(ctx)
	var retErr error
	if obj.Spec.Prune && obj.Status.Inventory != nil && obj.Status.Inventory.Entries != nil {
		objects, _ := inventory.List(obj.Status.Inventory)

		for _, object := range objects {
			if err := r.Client.Delete(ctx, object); err != nil {
				if !apierrors.IsNotFound(err) {
					logger.Error(err, "failed to delete object", "object", object)
					retErr = errors.Join(retErr, err)
				}
			}
		}

		if retErr != nil {
			return retErr
		}
	}

	// Remove our finalizer from the list and update it
	controllerutil.RemoveFinalizer(obj, mpasv1alpha1.ProjectFinalizer)

	return nil
}

func (r *ProjectReconciler) prune(ctx context.Context, obj *mpasv1alpha1.Project, staleObjects []*unstructured.Unstructured) error {
	logger := log.FromContext(ctx)
	var retErr error

	if !obj.Spec.Prune {
		return nil
	}

	for _, object := range staleObjects {
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(object), object); err != nil {
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "failed to get object for deletion")
				retErr = errors.Join(retErr, err)
			}
		}

		if err := r.Client.Delete(ctx, object); err != nil {
			logger.Error(err, "failed to delete object", "object", object)
			retErr = errors.Join(retErr, err)
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
	var opts []patch.Option
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

func (r *ProjectReconciler) reconcileCertificate(ctx context.Context, obj *mpasv1alpha1.Project) (*certmanagerv1.Certificate, error) {
	namespace := obj.GetNameWithPrefix(r.Prefix)
	issuerName := r.IssuerName

	// Note: Using unstructured here, because cert-manager does not expose their APIs.
	cert := &certmanagerv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocm-registry-tls-certs",
			Namespace: namespace,
		},
	}

	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, cert, func() error {
		// Make sure the fields are all up-to-date
		const keySize = 256
		cert.Spec = certmanagerv1.CertificateSpec{
			DNSNames:   []string{r.RegistryAddr},
			SecretName: "ocm-registry-tls-certs",
			IssuerRef: v1.ObjectReference{
				Name:  issuerName,
				Kind:  "ClusterIssuer",
				Group: "cert-manager.io",
			},
			PrivateKey: &certmanagerv1.CertificatePrivateKey{
				Algorithm: certmanagerv1.ECDSAKeyAlgorithm,
				Size:      keySize,
			},
		}

		return nil
	}); err != nil {
		return nil, fmt.Errorf("failed to create certificate request in namespace: %w", err)
	}

	return cert, nil
}

func (r *ProjectReconciler) applyMandatoryLabels(name, component, instance string, labels map[string]string) {
	labels[labelComponent] = component
	labels[labelInstance] = instance
	labels[labelCreatedBy] = ControllerName
	labels[labelManagedBy] = ControllerName
	labels[labelPartOf] = ControllerName
	labels[labelName] = name
}

func (r *ProjectReconciler) reconcileInventory(ctx context.Context, obj *mpasv1alpha1.Project) ([]runtime.Object, error) {
	var result []runtime.Object

	ns, err := r.reconcileNamespace(ctx, obj)
	if err != nil {
		r.markStalled(mpasv1alpha1.NamespaceCreateOrUpdateFailedReason, obj, err)

		return nil, fmt.Errorf("error reconciling namespace: %w", err)
	}

	sa, err := r.reconcileServiceAccount(ctx, obj)
	if err != nil {
		r.markStalled(mpasv1alpha1.ServiceAccountCreateOrUpdateFailedReason, obj, err)

		return nil, fmt.Errorf("error reconciling service account: %w", err)
	}

	role, err := r.reconcileRole(ctx, obj)
	if err != nil {
		r.markStalled(mpasv1alpha1.RBACCreateOrUpdateFailedReason, obj, err)

		return nil, fmt.Errorf("error reconciling project namespace role: %w", err)
	}

	roleBindings, err := r.reconcileRoleBindings(ctx, obj, sa)
	if err != nil {
		r.markStalled(mpasv1alpha1.RBACCreateOrUpdateFailedReason, obj, err)

		return nil, fmt.Errorf("error reconciling role bindings: %w", err)
	}

	certificate, err := r.reconcileCertificate(ctx, obj)
	if err != nil {
		r.markStalled(mpasv1alpha1.CertificateCreateOrUpdateFailedReason, obj, err)

		return nil, fmt.Errorf("error reconciling certificate: %w", err)
	}

	repo, err := r.reconcileRepository(ctx, obj)
	if err != nil {
		r.markStalled(mpasv1alpha1.RepositoryCreateOrUpdateFailedReason, obj, err)

		return nil, fmt.Errorf("error reconciling repository: %w", err)
	}

	obj.Status.RepositoryRef = &meta.NamespacedObjectReference{
		Name:      repo.GetName(),
		Namespace: repo.GetNamespace(),
	}

	gitRepo, err := r.reconcileFluxGitRepository(ctx, obj, repo)
	if err != nil {
		r.markStalled(mpasv1alpha1.FluxGitRepositoryCreateOrUpdateFailedReason, obj, err)

		return nil, fmt.Errorf("error reconciling flux git source: %w", err)
	}

	kustomizations, err := r.reconcileFluxKustomizations(ctx, obj)
	if err != nil {
		r.markStalled(mpasv1alpha1.FluxKustomizationsCreateOrUpdateFailedReason, obj, err)

		return nil, fmt.Errorf("error reconciling flux kustomizations: %w", err)
	}

	result = append(result, ns, sa, role, certificate, repo, gitRepo)

	for _, k := range kustomizations {
		result = append(result, k)
	}

	for _, r := range roleBindings {
		result = append(result, r)
	}

	return result, nil
}
