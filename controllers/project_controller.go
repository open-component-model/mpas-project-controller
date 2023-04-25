// SPDX-FileCopyrightText: 2022 SAP SE or an SAP affiliate company and Open Component Model contributors.
//
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"errors"
	"fmt"

	"github.com/fluxcd/pkg/apis/meta"
	"github.com/fluxcd/pkg/runtime/conditions"
	"github.com/fluxcd/pkg/runtime/patch"
	rreconcile "github.com/fluxcd/pkg/runtime/reconcile"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

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

	patchHelper, err := patch.NewHelper(obj, r.Client)
	if err != nil {
		retErr = errors.Join(retErr, err)
		return result, retErr
	}

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

	result, retErr = r.reconcile(ctx, obj)

	return ctrl.Result{}, nil
}

func (r *ProjectReconciler) reconcile(ctx context.Context, obj *mpasv1alpha1.Project) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	rreconcile.ProgressiveStatus(false, obj, meta.ProgressingReason, "reconciliation in progress")

	if obj.Generation != obj.Status.ObservedGeneration {
		rreconcile.ProgressiveStatus(false, obj, meta.ProgressingReason,
			"processing object: new generation %d -> %d", obj.Status.ObservedGeneration, obj.Generation)
	}

	conditions.Delete(obj, meta.StalledCondition)

	if nsReady := conditions.Get(obj, mpasv1alpha1.NamespaceReadyCondition); nsReady == nil || nsReady.Status != metav1.ConditionTrue {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: obj.GetName(),
			},
		}

		if err := r.Create(ctx, ns); err != nil {
			log.Error(err, "failed to create namespace with error: %w", err)
			conditions.MarkStalled(obj, mpasv1alpha1.NamespaceCreationFailedReason, err.Error())
			conditions.MarkFalse(obj, meta.ReadyCondition, mpasv1alpha1.NamespaceCreationFailedReason, err.Error())
			return ctrl.Result{}, err
		}

		conditions.MarkTrue(obj, mpasv1alpha1.NamespaceReadyCondition, meta.SucceededReason, "Namespace created")
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
