package guardian

import (
	"context"
	"errors"
	"testing"
)

// Kubernetes readiness must describe the generation the API currently serves,
// not a stale controller status left over from an earlier deployment version.
func TestValidationRejectsStaleReadiness(t *testing.T) {
	rec := &recorder{output: []byte(`{"metadata":{"uid":"u","name":"api","namespace":"demo","generation":9},"spec":{"replicas":2},"status":{"observedGeneration":8,"readyReplicas":2,"updatedReplicas":2,"availableReplicas":2}}`)}
	d, err := (Kubernetes{Context: "kind-guardian", Runner: rec}).Read(context.Background(), "demo", "api")
	if err != nil { t.Fatal(err) }
	if d.Ready != 0 { t.Fatalf("stale controller generation counted as recovered: ready=%d", d.Ready) }
}

func TestValidationRejectsUnavailableReplicas(t *testing.T) {
	rec := &recorder{output: []byte(`{"metadata":{"uid":"u","name":"api","namespace":"demo","generation":9},"spec":{"replicas":2},"status":{"observedGeneration":9,"readyReplicas":2,"updatedReplicas":2,"availableReplicas":0}}`)}
	d, err := (Kubernetes{Context: "kind-guardian", Runner: rec}).Read(context.Background(), "demo", "api")
	if err != nil { t.Fatal(err) }
	if d.Ready != 0 { t.Fatalf("unavailable rollout counted as recovered: ready=%d", d.Ready) }
}

type rollbackReadFailure struct { *fakeCluster }
func (c rollbackReadFailure) Read(ctx context.Context, ns, name string) (Deployment, error) {
	if c.writes >= 2 { return Deployment{}, errors.New("cannot confirm rollback state") }
	return c.fakeCluster.Read(ctx, ns, name)
}

func TestValidationDoesNotInventRollbackObservation(t *testing.T) {
	r, cluster, _ := fixture()
	cluster.readyImmediately = false
	r.Cluster = rollbackReadFailure{cluster}
	r.Execute = true
	r.Approval = "scale:demo/api:2:uid-1234"
	result, err := r.Run(context.Background())
	if err == nil { t.Fatal("unconfirmed rollback must return an error") }
	if result.Status == "rolled_back" { t.Fatal("reported rollback success without observing the state after the write") }
	if result.After.Replicas == 0 { t.Fatal("fabricated after-state by copying the before-state") }
}
