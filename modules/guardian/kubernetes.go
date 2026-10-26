package guardian

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// CommandRunner makes command execution testable; arguments never pass a shell.
type CommandRunner interface {
	Run(context.Context, ...string) ([]byte, error)
}

type KubectlCommand struct{}

func (KubectlCommand) Run(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	return cmd.CombinedOutput()
}

type Kubernetes struct {
	Context string
	Runner CommandRunner
}

var kubeContextPattern = regexp.MustCompile(`^[A-Za-z0-9_./:@-]{1,180}$`)

func (k Kubernetes) command(ctx context.Context, args ...string) ([]byte, error) {
	if !kubeContextPattern.MatchString(k.Context) || strings.HasPrefix(k.Context, "-") || strings.Contains(k.Context, "..") {
		return nil, errors.New("explicit safe kubectl context required")
	}
	runner := k.Runner
	if runner == nil { runner = KubectlCommand{} }
	c, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	all := append([]string{"--context", k.Context, "--request-timeout=15s"}, args...)
	output, err := runner.Run(c, all...)
	if err != nil { return nil, fmt.Errorf("kubectl command failed: %w", err) }
	if len(output) > 1<<20 { return nil, errors.New("oversized kubectl response") }
	return output, nil
}

func (k Kubernetes) Read(ctx context.Context, namespace, name string) (Deployment, error) {
	if !ValidName(namespace) || !ValidName(name) {
		return Deployment{}, errors.New("invalid namespace or name")
	}
	out, err := k.command(ctx, "-n", namespace, "get", "deployment", name, "-o", "json")
	if err != nil { return Deployment{}, err }
	var raw struct {
		Metadata struct {
			UID string `json:"uid"`
			Name string `json:"name"`
			Namespace string `json:"namespace"`
			Generation int64 `json:"generation"`
		} `json:"metadata"`
		Spec struct { Replicas *int32 `json:"replicas"` } `json:"spec"`
		Status struct {
			Ready int32 `json:"readyReplicas"`
			Updated int32 `json:"updatedReplicas"`
			Available int32 `json:"availableReplicas"`
			ObservedGeneration int64 `json:"observedGeneration"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out, &raw); err != nil { return Deployment{}, fmt.Errorf("parse deployment: %w", err) }
	if raw.Metadata.UID == "" || raw.Metadata.Name != name || raw.Metadata.Namespace != namespace || raw.Spec.Replicas == nil {
		return Deployment{}, errors.New("deployment missing identity or replica fields")
	}
	// Ready is a confirmed rollout-ready count, not an unqualified copy of
	// status.readyReplicas. The controller must have observed this generation,
	// and the desired replicas must be updated and available. A stale status
	// or missing freshness evidence cannot demonstrate recovery.
	ready := raw.Status.Ready
	if raw.Metadata.Generation <= 0 || raw.Status.ObservedGeneration < raw.Metadata.Generation ||
		raw.Status.Updated < *raw.Spec.Replicas || raw.Status.Available < *raw.Spec.Replicas {
		ready = 0
	}
	return Deployment{Name:name, Namespace:namespace, UID:raw.Metadata.UID, Replicas:*raw.Spec.Replicas, Ready:ready}, nil
}

func (k Kubernetes) Scale(ctx context.Context, namespace, name, uid string, from, to int32) error {
	if !ValidName(namespace) || !ValidName(name) || uid == "" || len(uid) > 128 || from < 0 || to < 0 || to > 10 || from > 10 {
		return errors.New("invalid guarded scale arguments")
	}
	// Kubernetes applies these JSON Patch tests and the replacement atomically.
	patch, err := json.Marshal([]map[string]any{
		{"op":"test","path":"/metadata/uid","value":uid},
		{"op":"test","path":"/spec/replicas","value":from},
		{"op":"replace","path":"/spec/replicas","value":to},
	})
	if err != nil { return err }
	_, err = k.command(ctx, "-n", namespace, "patch", "deployment", name, "--type=json", "-p", string(patch))
	return err
}

// readDeploymentJSON is intentionally bounded and independent of CLI stderr.
func readDeploymentJSON(data []byte) (map[string]any, error) {
	if len(data) > 1<<20 { return nil, errors.New("deployment payload too large") }
	var doc map[string]any
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&doc); err != nil { return nil, err }
	return doc, nil
}
