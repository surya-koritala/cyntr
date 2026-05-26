package policy

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-policy-agent/opa/v1/rego"
)

// RegoEvaluator wraps a compiled OPA query that resolves to
// data.cyntr.policy.decision. It is the policy-as-code complement
// to the YAML RuleSet — same CheckRequest in, same CheckResponse out.
type RegoEvaluator struct {
	query   rego.PreparedEvalQuery
	sources []string // file paths loaded (for diagnostics / health)
}

// LoadRegoPolicy loads .rego policies from a file or directory and
// compiles a prepared query for data.cyntr.policy.decision.
//
// Behavior:
//   - path == "" → returns (nil, nil); caller treats nil as disabled
//   - path is a directory → loads every *.rego file in it (non-recursive)
//   - path is a single file → loads that file
//   - no .rego files found at the path → returns (nil, nil) (disabled)
//   - any parse / compile error → returns (nil, err)
func LoadRegoPolicy(path string) (*RegoEvaluator, error) {
	if path == "" {
		return nil, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("rego policy path %s: %w", path, err)
	}

	var files []string
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, fmt.Errorf("read rego dir %s: %w", path, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if !strings.HasSuffix(entry.Name(), ".rego") {
				continue
			}
			files = append(files, filepath.Join(path, entry.Name()))
		}
	} else {
		if !strings.HasSuffix(path, ".rego") {
			return nil, nil
		}
		files = []string{path}
	}

	if len(files) == 0 {
		return nil, nil
	}

	opts := []func(*rego.Rego){
		rego.Query("data.cyntr.policy.decision"),
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read rego file %s: %w", f, err)
		}
		opts = append(opts, rego.Module(f, string(data)))
	}

	ctx := context.Background()
	prepared, err := rego.New(opts...).PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("compile rego policy: %w", err)
	}

	return &RegoEvaluator{query: prepared, sources: files}, nil
}

// Evaluate runs the prepared Rego query with req as input and maps the
// resulting decision string ("allow" | "deny" | "require_approval") to
// a CheckResponse. Any error — query failure, no result, unrecognized
// decision — fails closed (Deny).
func (e *RegoEvaluator) Evaluate(ctx context.Context, req CheckRequest) CheckResponse {
	if e == nil {
		// caller treats nil evaluator as disabled — never expected here, fail closed.
		return CheckResponse{Decision: Deny, Rule: "rego", Reason: "rego evaluator is nil"}
	}

	input := map[string]any{
		"tenant": req.Tenant,
		"action": req.Action,
		"tool":   req.Tool,
		"agent":  req.Agent,
		"user":   req.User,
	}

	results, err := e.query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return CheckResponse{Decision: Deny, Rule: "rego", Reason: fmt.Sprintf("rego eval error: %v", err)}
	}
	if len(results) == 0 || len(results[0].Expressions) == 0 {
		return CheckResponse{Decision: Deny, Rule: "rego", Reason: "rego returned no decision"}
	}

	raw := results[0].Expressions[0].Value
	decStr, ok := raw.(string)
	if !ok {
		return CheckResponse{Decision: Deny, Rule: "rego", Reason: fmt.Sprintf("rego decision not a string: %T", raw)}
	}

	switch decStr {
	case "allow":
		return CheckResponse{Decision: Allow, Rule: "rego", Reason: "rego policy allowed"}
	case "deny":
		return CheckResponse{Decision: Deny, Rule: "rego", Reason: "rego policy denied"}
	case "require_approval":
		return CheckResponse{Decision: RequireApproval, Rule: "rego", Reason: "rego policy requires approval"}
	default:
		return CheckResponse{Decision: Deny, Rule: "rego", Reason: fmt.Sprintf("rego decision unrecognized: %q", decStr)}
	}
}

// Sources returns the file paths the evaluator was compiled from.
func (e *RegoEvaluator) Sources() []string {
	if e == nil {
		return nil
	}
	out := make([]string, len(e.sources))
	copy(out, e.sources)
	return out
}
