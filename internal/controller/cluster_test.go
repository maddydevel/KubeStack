// Copyright 2026 The KubeStack Authors. All rights reserved.

package controller

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	clientfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	"kubestack.dev/kubestack/api/v1alpha1"
	"kubestack.dev/kubestack/internal/audit"
	"kubestack.dev/kubestack/internal/engine"
	"kubestack.dev/kubestack/internal/engine/fake"
	"kubestack.dev/kubestack/internal/store"
	"kubestack.dev/kubestack/internal/upgrade"
)

type env struct {
	r   *ClusterReconciler
	eng *fake.Engine
	st  *store.Memory
}

func setup(t *testing.T, objs ...client.Object) env {
	t.Helper()
	sch := runtime.NewScheme()
	if err := v1alpha1.AddToScheme(sch); err != nil {
		t.Fatal(err)
	}
	c := clientfake.NewClientBuilder().WithScheme(sch).WithObjects(objs...).WithStatusSubresource(&v1alpha1.KubeStackCluster{}).Build()
	e := env{eng: fake.Demo(), st: store.NewMemory()}
	e.r = &ClusterReconciler{Client: c, Engine: e.eng, Audit: e.st, Now: time.Now}
	return e
}

func ksc(name, tier, version, policy string) *v1alpha1.KubeStackCluster {
	return &v1alpha1.KubeStackCluster{ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.KubeStackClusterSpec{Tier: tier, KubernetesVersion: version, DeletionPolicy: policy}}
}

func (e env) reconcile(t *testing.T, name string) *v1alpha1.KubeStackCluster {
	t.Helper()
	ctx := context.Background()
	if _, err := e.r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: name}}); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	var obj v1alpha1.KubeStackCluster
	if err := e.r.Client.Get(ctx, types.NamespacedName{Name: name}, &obj); err != nil {
		return nil // gone
	}
	return &obj
}

func synced(t *testing.T, obj *v1alpha1.KubeStackCluster) *metav1.Condition {
	t.Helper()
	c := meta.FindStatusCondition(obj.Status.Conditions, v1alpha1.ConditionSynced)
	if c == nil {
		t.Fatal("no Synced condition")
	}
	return c
}

func actions(t *testing.T, st *store.Memory) []string {
	t.Helper()
	recs, err := st.ListAudit(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range recs {
		if r.Actor != actor {
			t.Fatalf("audit actor %q", r.Actor)
		}
		out = append(out, r.Action+":"+r.Result)
	}
	slices.Sort(out) // the store lists newest first
	return out
}

func TestCreateProvisioned(t *testing.T) {
	v := upgrade.Versions()[0]
	e := setup(t, ksc("new-one", "provisioned", v, ""))
	obj := e.reconcile(t, "new-one")
	if c := synced(t, obj); c.Status != metav1.ConditionTrue {
		t.Fatalf("synced: %+v", c)
	}
	if len(obj.Finalizers) != 1 || obj.Status.Tier != "provisioned" || obj.Status.KubernetesVersion != v {
		t.Fatalf("object: %+v", obj)
	}
	if _, err := e.eng.GetCluster(context.Background(), "new-one"); err != nil {
		t.Fatalf("engine: %v", err)
	}
	if a := strings.Join(actions(t, e.st), ","); !strings.Contains(a, "create:attempt") || !strings.Contains(a, "create:success") {
		t.Fatalf("audit: %s", a)
	}
	// Second pass is a no-op: no further lifecycle operations.
	e.reconcile(t, "new-one")
	if n := len(actions(t, e.st)); n != 2 {
		t.Fatalf("audit records after resync: %d", n)
	}
}

func TestInvalidVersionNotCreated(t *testing.T) {
	e := setup(t, ksc("bad", "provisioned", "v9.9.9+rke2r1", ""))
	obj := e.reconcile(t, "bad")
	if c := synced(t, obj); c.Status != metav1.ConditionFalse || !strings.Contains(c.Message, "release catalog") {
		t.Fatalf("synced: %+v", c)
	}
	if _, err := e.eng.GetCluster(context.Background(), "bad"); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("engine: %v", err)
	}
	if a := actions(t, e.st); len(a) != 0 {
		t.Fatalf("audit: %v", a)
	}
}

func TestImportAttached(t *testing.T) {
	e := setup(t, ksc("edge", "attached", "", ""))
	if obj := e.reconcile(t, "edge"); synced(t, obj).Status != metav1.ConditionTrue || obj.Status.Tier != "attached" {
		t.Fatalf("object: %+v", obj.Status)
	}
	if a := strings.Join(actions(t, e.st), ","); !strings.Contains(a, "import:success") {
		t.Fatalf("audit: %s", a)
	}
}

func TestAdoptAndUpgrade(t *testing.T) {
	e := setup(t, ksc("prod-eu", "provisioned", "v1.35.9+rke2r1", ""))
	obj := e.reconcile(t, "prod-eu")
	if c := synced(t, obj); c.Status != metav1.ConditionTrue {
		t.Fatalf("synced: %+v", c)
	}
	if c, _ := e.eng.GetCluster(context.Background(), "prod-eu"); c.KubernetesVersion != "v1.35.9+rke2r1" {
		t.Fatalf("engine not upgraded: %+v", c)
	}
	if a := strings.Join(actions(t, e.st), ","); !strings.Contains(a, "upgrade:success") || strings.Contains(a, "create") {
		t.Fatalf("audit: %s", a)
	}
}

func TestUpgradeBlocked(t *testing.T) {
	for name, tc := range map[string]struct{ obj *v1alpha1.KubeStackCluster }{
		"skips a minor":       {ksc("prod-eu", "provisioned", upgrade.Versions()[len(upgrade.Versions())-1], "")},
		"tier mismatch":       {ksc("legacy-k3s", "provisioned", "v1.35.9+rke2r1", "")},
		"not upgrade capable": {ksc("legacy-k3s", "attached", "v1.32.0+k3s1", "")},
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t, tc.obj)
			before, _ := e.eng.GetCluster(context.Background(), tc.obj.Name)
			if c := synced(t, e.reconcile(t, tc.obj.Name)); c.Status != metav1.ConditionFalse {
				t.Fatalf("synced: %+v", c)
			}
			if after, _ := e.eng.GetCluster(context.Background(), tc.obj.Name); after != before {
				t.Fatalf("cluster changed: %+v", after)
			}
			if a := actions(t, e.st); len(a) != 0 {
				t.Fatalf("audit: %v", a)
			}
		})
	}
}

func TestDeletion(t *testing.T) {
	for _, policy := range []string{v1alpha1.DeletionRetain, v1alpha1.DeletionDelete} {
		t.Run(policy, func(t *testing.T) {
			ctx := context.Background()
			e := setup(t, ksc("prod-eu", "provisioned", "", policy))
			obj := e.reconcile(t, "prod-eu")
			if err := e.r.Client.Delete(ctx, obj); err != nil {
				t.Fatal(err)
			}
			if obj := e.reconcile(t, "prod-eu"); obj != nil {
				t.Fatalf("resource not released: %+v", obj)
			}
			_, err := e.eng.GetCluster(ctx, "prod-eu")
			if gone := errors.Is(err, engine.ErrNotFound); gone != (policy == v1alpha1.DeletionDelete) {
				t.Fatalf("policy %s: engine err %v", policy, err)
			}
			want := ""
			if policy == v1alpha1.DeletionDelete {
				want = "delete:attempt,delete:success"
			}
			if a := strings.Join(actions(t, e.st), ","); a != want {
				t.Fatalf("audit: %q", a)
			}
		})
	}
}

func TestDeleteBlockedForHosted(t *testing.T) {
	ctx := context.Background()
	o := ksc("analytics-eks", "attached", "", v1alpha1.DeletionDelete) // tier mismatch is irrelevant to the delete gate
	e := setup(t, o)
	obj := e.reconcile(t, "analytics-eks")
	if err := e.r.Client.Delete(ctx, obj); err != nil {
		t.Fatal(err)
	}
	obj = e.reconcile(t, "analytics-eks")
	if obj == nil || !strings.Contains(synced(t, obj).Message, "delete not permitted") {
		t.Fatalf("hosted cluster delete not blocked: %+v", obj)
	}
	if _, err := e.eng.GetCluster(ctx, "analytics-eks"); err != nil {
		t.Fatalf("engine: %v", err)
	}
}

func TestAuditUnavailableRefuses(t *testing.T) {
	e := setup(t, ksc("new-one", "attached", "", ""))
	e.r.Audit = failingSink{}
	if c := synced(t, e.reconcile(t, "new-one")); !strings.Contains(c.Message, audit.ErrUnavailable.Error()) {
		t.Fatalf("synced: %+v", c)
	}
	if _, err := e.eng.GetCluster(context.Background(), "new-one"); !errors.Is(err, engine.ErrNotFound) {
		t.Fatalf("imported without audit: %v", err)
	}
}

type failingSink struct{}

func (failingSink) AddAudit(context.Context, audit.Record) error { return errors.New("db down") }
