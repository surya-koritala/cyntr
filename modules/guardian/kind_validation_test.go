package guardian

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestGuardianKindValidation runs only in an explicitly named disposable cluster.
// Mock-model runs prove real Kubernetes behavior, NOT live Nebius inference.
func TestGuardianKindValidation(t *testing.T) {
	if os.Getenv("GUARDIAN_KIND_VALIDATION") != "1" {
		t.Skip("opt-in: requires disposable kind-guardian-validation cluster")
	}
	const clusterContext = "kind-guardian-validation"
	if os.Getenv("GUARDIAN_TEST_CONTEXT") != clusterContext {
		t.Fatal("GUARDIAN_TEST_CONTEXT must be kind-guardian-validation; refusing other clusters")
	}
	mode := os.Getenv("GUARDIAN_MODEL_MODE")
	if mode != "mock" && mode != "live" { t.Fatal("GUARDIAN_MODEL_MODE must be mock or live") }
	if mode == "live" && os.Getenv("NEBIUS_API_KEY") == "" { t.Fatal("live mode requires NEBIUS_API_KEY; no mock fallback") }
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Minute)
	defer cancel()
	namespace := fmt.Sprintf("guardian-validation-%d", time.Now().UnixNano())
	kube := func(input string, args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "kubectl", append([]string{"--context", clusterContext, "--request-timeout=20s"}, args...)...)
		if input != "" { cmd.Stdin = strings.NewReader(input) }
		return cmd.CombinedOutput()
	}
	if output, err := kube("", "create", "namespace", namespace); err != nil { t.Fatalf("create isolated namespace: %v: %s", err, output) }
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		cmd := exec.CommandContext(cleanupCtx, "kubectl", "--context", clusterContext, "delete", "namespace", namespace, "--wait=false")
		if output, err := cmd.CombinedOutput(); err != nil { t.Errorf("cleanup namespace: %v: %s", err, output) }
	})
	outDir := os.Getenv("GUARDIAN_EVIDENCE_DIR")
	if outDir == "" { outDir = t.TempDir() }
	if err := os.MkdirAll(outDir, 0700); err != nil { t.Fatal(err) }
	writeJSON := func(name string, value any) {
		data, err := json.MarshalIndent(value, "", "  ")
		if err == nil { err = os.WriteFile(filepath.Join(outDir, name), data, 0600) }
		if err != nil { t.Errorf("write evidence %s: %v", name, err) }
	}
	evidence := map[string]any{"model_mode": mode, "source_sha": os.Getenv("GITHUB_SHA"), "cluster_context": clusterContext, "namespace": namespace, "started_at": time.Now().UTC(), "live_inference_verified": false}
	defer func() {
		evidence["finished_at"] = time.Now().UTC()
		evidence["passed"] = !t.Failed()
		if mode == "live" && !t.Failed() { evidence["live_inference_verified"] = true }
		writeJSON("validation-summary.json", evidence)
	}()
	planner := NebiusPlanner{APIKey: os.Getenv("NEBIUS_API_KEY"), Model: DefaultNebiusModel}
	if mode == "mock" {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" { http.NotFound(w, r); return }
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"model":"MOCK-NOT-LIVE-INFERENCE","choices":[{"message":{"content":"{\"action\":\"restore_replicas\",\"desired_replicas\":1,\"explanation\":\"MOCK validation fixture: restore one replica\"}"}}],"usage":{"prompt_tokens":0,"completion_tokens":0}}`)
		}))
		defer server.Close()
		planner = NebiusPlanner{APIKey:"mock-only-not-a-secret", BaseURL:server.URL+"/v1", Model:"MOCK-NOT-LIVE-INFERENCE", Client:server.Client()}
	}
	cluster := Kubernetes{Context:clusterContext}
	for _, name := range []string{"web", "broken"} {
		container := map[string]any{"name":"web", "image":"nginx:1.27.5-alpine", "imagePullPolicy":"IfNotPresent", "ports":[]map[string]any{{"containerPort":80}}, "readinessProbe":map[string]any{"httpGet":map[string]any{"path":"/", "port":80}, "periodSeconds":1}, "resources":map[string]any{"requests":map[string]string{"cpu":"20m", "memory":"32Mi"}, "limits":map[string]string{"cpu":"200m", "memory":"128Mi"}}}
		if name == "broken" { container["command"] = []string{"/bin/sh", "-c", "exit 1"} }
		manifest := map[string]any{"apiVersion":"apps/v1", "kind":"Deployment", "metadata":map[string]string{"name":name,"namespace":namespace}, "spec":map[string]any{"replicas":0, "minReadySeconds":2, "selector":map[string]any{"matchLabels":map[string]string{"app":name}}, "template":map[string]any{"metadata":map[string]any{"labels":map[string]string{"app":name}}, "spec":map[string]any{"automountServiceAccountToken":false,"containers":[]any{container}}}}}
		data, err := json.Marshal(manifest)
		if err != nil { t.Fatal(err) }
		if output, err := kube(string(data), "create", "-f", "-"); err != nil { t.Fatalf("fixture %s: %v: %s",name,err,output) }
	}
	run := func(name string, execute bool, approval string, timeout time.Duration, label string) (Result, error) {
		r := Runner{Cluster:cluster, Planner:planner, Namespace:namespace, Deployment:name, Symptom:"Synthetic test incident: service unavailable following scale-to-zero; previous intended count was one. Propose restoring exactly one replica.", MaxReplicas:3, Execute:execute, Approval:approval, VerifyTimeout:timeout, PollInterval:time.Second}
		result, err := r.Run(ctx)
		if err != nil { result.Error = err.Error() }
		writeJSON(label+".json", map[string]any{"model_mode":mode,"result":result})
		if mode == "live" {
			page, renderErr := HTMLReport(result)
			if renderErr == nil { renderErr = os.WriteFile(filepath.Join(outDir,label+".html"),page,0600) }
			if renderErr != nil { t.Errorf("HTML evidence: %v",renderErr) }
		}
		t.Logf("mode=%s case=%s status=%s elapsed_ms=%d", mode,label,result.Status,result.DurationMs)
		return result,err
	}
	plan, err := run("web",false,"",120*time.Second,"dry-run")
	if err != nil || plan.Status != "planned" { t.Fatalf("dry run failed: status=%s error=%v",plan.Status,err) }
	state, err := cluster.Read(ctx,namespace,"web")
	if err != nil || state.Replicas != 0 { t.Fatalf("dry run mutated fixture: %+v %v",state,err) }
	denied, err := run("web",true,"intentionally-invalid-approval",120*time.Second,"denied")
	if err == nil || denied.Status != "denied" { t.Fatalf("expected denied: %+v %v",denied,err) }
	state, err = cluster.Read(ctx,namespace,"web")
	if err != nil || state.Replicas != 0 { t.Fatalf("denied run mutated fixture: %+v %v",state,err) }
	if err := cluster.Scale(ctx,namespace,"web","wrong-immutable-uid",0,1); err == nil { t.Fatal("real API accepted wrong UID") }
	state, err = cluster.Read(ctx,namespace,"web")
	if err != nil || state.Replicas != 0 { t.Fatalf("UID precondition failure mutated fixture: %+v %v",state,err) }
	evidence["uid_precondition_verified"] = true
	recovered, err := run("web",true,plan.ApprovalRequired,120*time.Second,"recovery")
	if err != nil || recovered.Status != "recovered" { t.Fatalf("recovery failed: status=%s error=%v",recovered.Status,err) }
	state, err = cluster.Read(ctx,namespace,"web")
	if err != nil || state.Ready != recovered.Proposal.DesiredReplicas { t.Fatalf("independent recovery readback: %+v %v",state,err) }
	// This independent fixture smoke check does not expand Guardian's tool policy.
	output, err := kube("","-n",namespace,"exec","deployment/web","--","wget","-q","-O","-","http://127.0.0.1/")
	if err != nil || !strings.Contains(string(output),"Welcome to nginx") { t.Fatalf("application HTTP smoke check failed: %v",err) }
	evidence["application_http_smoke_passed"] = true
	brokenPlan, err := run("broken",false,"",8*time.Second,"rollback-plan")
	if err != nil || brokenPlan.Status != "planned" { t.Fatalf("rollback planning failed: %v",err) }
	rolledBack, err := run("broken",true,brokenPlan.ApprovalRequired,8*time.Second,"rollback")
	if err == nil || rolledBack.Status != "rolled_back" { t.Fatalf("expected failed recovery with confirmed rollback: status=%s error=%v",rolledBack.Status,err) }
	state, err = cluster.Read(ctx,namespace,"broken")
	if err != nil || state.Replicas != 0 { t.Fatalf("independent rollback readback: %+v %v",state,err) }
	for _, name := range []string{"web","broken"} {
		output, err := kube("","-n",namespace,"get","deployment",name,"-o","json")
		if err != nil { t.Fatal(err) }
		var actual any
		if err := json.Unmarshal(output,&actual); err != nil { t.Fatal(err) }
		writeJSON(name+"-actual-deployment.json",actual)
	}
}
