package main

import (
    "context"
    "encoding/json"
    "errors"
    "flag"
    "fmt"
    "os"
    "time"

    "github.com/cyntr-dev/cyntr/modules/guardian"
)

// runGuardian implements `cyntr guardian`, an opt-in, guarded, dry-run-first
// recovery workflow. The default Cyntr Kubernetes read tool is unchanged.
func runGuardian(args []string) error {
    f := flag.NewFlagSet("guardian", flag.ContinueOnError)
    namespace := f.String("namespace", "", "explicit Kubernetes namespace")
    deployment := f.String("deployment", "", "deployment name")
    kubeContext := f.String("context", "", "explicit Kubernetes context (required)")
    symptom := f.String("symptom", "HTTP service unavailable following accidental scale-down", "observed incident symptom")
    maxReplicas := f.Int("max-replicas", 3, "upper bound on proposed replicas (1..10)")
    execute := f.Bool("execute", false, "perform approved mutation; default is dry-run")
    approval := f.String("approve", "", "explicit action-bound approval, e.g. scale:demo/api:2")
    verify := f.Duration("verify-timeout", 60*time.Second, "health verification deadline")
    model := f.String("model", guardian.DefaultNebiusModel, "NVIDIA model ID served by Nebius")
    base := f.String("nebius-base-url", guardian.DefaultNebiusBaseURL, "Token Factory API base ending in /v1")
    if err := f.Parse(args); err != nil { return err }
    if len(f.Args()) != 0 { return errors.New("unexpected positional arguments") }
    if !guardian.ValidName(*namespace) || !guardian.ValidName(*deployment) || *kubeContext=="" {
        return errors.New("--namespace, --deployment, and --context are required")
    }
    if *maxReplicas < 1 || *maxReplicas > 10 { return errors.New("--max-replicas must be 1..10") }
    if *execute && *approval=="" { return errors.New("--execute requires --approve with exact planned action") }
    ctx, cancel := context.WithTimeout(context.Background(), *verify+75*time.Second)
    defer cancel()
    runner := guardian.Runner{
        Cluster:guardian.Kubernetes{Context:*kubeContext},
        Planner:guardian.NebiusPlanner{APIKey:os.Getenv("NEBIUS_API_KEY"),BaseURL:*base,Model:*model},
        Namespace:*namespace,Deployment:*deployment,Symptom:*symptom,
        MaxReplicas:int32(*maxReplicas),Execute:*execute,Approval:*approval,
        VerifyTimeout:*verify,
    }
    result, err := runner.Run(ctx)
    output, marshalErr := json.MarshalIndent(result,"","  ")
    if marshalErr==nil { fmt.Println(string(output)) }
    if err != nil { return fmt.Errorf("guardian ended with status %q: %w",result.Status,err) }
    return nil
}
