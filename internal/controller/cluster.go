// Copyright 2026 The KubeStack Authors. All rights reserved.

// Package controller reconciles KubeStack CRDs to the lifecycle engine. Engine-neutral: it only uses
// engine.Engine, with the same capability gate, pre-flight checks and audit trail as the API.
package controller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"kubestack.dev/kubestack/api/v1alpha1"
	"kubestack.dev/kubestack/internal/audit"
	"kubestack.dev/kubestack/internal/domain"
	"kubestack.dev/kubestack/internal/engine"
	"kubestack.dev/kubestack/internal/upgrade"
)

const (
	finalizer = "kubestack.dev/cluster"
	// Actor in audit records. Who may edit KubeStackClusters is governed by Kubernetes RBAC on the
	// management cluster; the editing user is not visible to the controller.
	actor = "system:kubestack-controller"
	// Status refresh and retry interval.
	resync = time.Minute
)

type ClusterReconciler struct {
	Client client.Client
	Engine engine.Engine
	Audit  audit.Sink
	OIDC   *domain.ClusterOIDC // applied to new provisioned clusters, as in the API
	Now    func() time.Time
}

func (r *ClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).For(&v1alpha1.KubeStackCluster{}).Complete(r)
}

func (r *ClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var obj v1alpha1.KubeStackCluster
	if err := r.Client.Get(ctx, req.NamespacedName, &obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !obj.DeletionTimestamp.IsZero() {
		return r.finalize(ctx, &obj)
	}
	if controllerutil.AddFinalizer(&obj, finalizer) {
		if err := r.Client.Update(ctx, &obj); err != nil {
			return ctrl.Result{}, err
		}
	}
	c, err := r.Engine.GetCluster(ctx, obj.Name)
	switch {
	case errors.Is(err, engine.ErrNotFound):
		c, err = r.create(ctx, &obj)
	case err == nil:
		err = r.converge(ctx, &obj, &c)
	}
	return r.status(ctx, &obj, c, err)
}

func (r *ClusterReconciler) create(ctx context.Context, obj *v1alpha1.KubeStackCluster) (c domain.Cluster, err error) {
	switch obj.Spec.Tier {
	case string(domain.TierProvisioned):
		req := domain.CreateRequest{Name: obj.Name, KubernetesVersion: obj.Spec.KubernetesVersion}
		if err := req.Validate(upgrade.Versions()); err != nil {
			return c, err
		}
		req.OIDC = r.OIDC
		err = audit.Run(ctx, r.Audit, actor, "create", req.Name, nil, req, func() (err error) {
			c, err = r.Engine.CreateCluster(ctx, req)
			return err
		})
	case string(domain.TierAttached):
		req := domain.ImportRequest{Name: obj.Name}
		if err := req.Validate(); err != nil {
			return c, err
		}
		err = audit.Run(ctx, r.Audit, actor, "import", req.Name, nil, req, func() (err error) {
			c, err = r.Engine.ImportCluster(ctx, req)
			return err
		})
	default:
		err = fmt.Errorf("unsupported tier %q", obj.Spec.Tier)
	}
	return c, err
}

// converge adopts an existing cluster of the same name and applies a version change.
func (r *ClusterReconciler) converge(ctx context.Context, obj *v1alpha1.KubeStackCluster, c *domain.Cluster) error {
	if string(c.Tier) != obj.Spec.Tier {
		return fmt.Errorf("cluster %q exists with tier %q, not %q", c.ID, c.Tier, obj.Spec.Tier)
	}
	target := obj.Spec.KubernetesVersion
	if target == "" || target == c.KubernetesVersion {
		return nil
	}
	// Same gate order as the API: capability and upgradeOwner before probing the cluster.
	if ok, why := c.Allowed(domain.OpUpgrade); !ok {
		return errors.New("upgrade not permitted: " + why)
	}
	snaps, err := r.Engine.Snapshots(ctx, c.ID)
	if err != nil {
		return err
	}
	nodes, err := r.Engine.Nodes(ctx, c.ID)
	if err != nil {
		return err
	}
	if f := upgrade.Check(*c, target, upgrade.PreflightFrom(snaps, nodes), r.Now()); len(f) > 0 {
		return errors.New("pre-flight checks failed: " + strings.Join(f, "; "))
	}
	after := *c
	after.KubernetesVersion = target
	if err := audit.Run(ctx, r.Audit, actor, "upgrade", c.ID, *c, after, func() error {
		return r.Engine.UpgradeCluster(ctx, c.ID, target)
	}); err != nil {
		return err
	}
	*c = after
	return nil
}

func (r *ClusterReconciler) finalize(ctx context.Context, obj *v1alpha1.KubeStackCluster) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(obj, finalizer) {
		return ctrl.Result{}, nil
	}
	if obj.Spec.DeletionPolicy == v1alpha1.DeletionDelete {
		c, err := r.Engine.GetCluster(ctx, obj.Name)
		if err == nil {
			if ok, why := c.Allowed(domain.OpDelete); !ok {
				// Stays blocked until the policy is changed to Retain.
				err = errors.New("delete not permitted: " + why)
			} else {
				err = audit.Run(ctx, r.Audit, actor, "delete", c.ID, c, nil, func() error { return r.Engine.DeleteCluster(ctx, c.ID) })
			}
		}
		if err != nil && !errors.Is(err, engine.ErrNotFound) {
			return r.status(ctx, obj, c, err)
		}
	}
	controllerutil.RemoveFinalizer(obj, finalizer)
	return ctrl.Result{}, r.Client.Update(ctx, obj)
}

// status records the observed cluster and the outcome, then requeues to refresh it.
func (r *ClusterReconciler) status(ctx context.Context, obj *v1alpha1.KubeStackCluster, c domain.Cluster, rerr error) (ctrl.Result, error) {
	cond := metav1.Condition{Type: v1alpha1.ConditionSynced, Status: metav1.ConditionTrue, Reason: "Reconciled", ObservedGeneration: obj.Generation}
	if rerr != nil {
		cond.Status, cond.Reason, cond.Message = metav1.ConditionFalse, "Error", rerr.Error()
	}
	if c.ID != "" {
		obj.Status.Tier, obj.Status.UpgradeOwner = string(c.Tier), string(c.UpgradeOwner)
		obj.Status.KubernetesVersion, obj.Status.Ready = c.KubernetesVersion, c.Ready
	}
	obj.Status.ObservedGeneration = obj.Generation
	meta.SetStatusCondition(&obj.Status.Conditions, cond)
	if err := r.Client.Status().Update(ctx, obj); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: resync}, nil
}
