package eval

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cyntr-dev/cyntr/kernel"
	"github.com/cyntr-dev/cyntr/kernel/ipc"
	"github.com/cyntr-dev/cyntr/kernel/log"
	"github.com/cyntr-dev/cyntr/modules/agent"
)

var logger = log.Default().WithModule("eval")

// Runner manages agent evaluations.
type Runner struct {
	bus     *ipc.Bus
	runs    map[string]*EvalRun
	mu      sync.RWMutex
	counter int64
}

func New() *Runner {
	return &Runner{runs: make(map[string]*EvalRun)}
}

func (r *Runner) Name() string           { return "eval" }
func (r *Runner) Dependencies() []string { return []string{"agent_runtime"} }

func (r *Runner) Init(ctx context.Context, svc *kernel.Services) error {
	r.bus = svc.Bus
	return nil
}

func (r *Runner) Start(ctx context.Context) error {
	r.bus.Handle("eval", "eval.run", r.handleRun)
	r.bus.Handle("eval", "eval.status", r.handleStatus)
	r.bus.Handle("eval", "eval.list", r.handleList)
	return nil
}

func (r *Runner) Stop(ctx context.Context) error { return nil }

func (r *Runner) Health(ctx context.Context) kernel.HealthStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return kernel.HealthStatus{Healthy: true, Message: fmt.Sprintf("%d eval runs", len(r.runs))}
}

func (r *Runner) handleRun(msg ipc.Message) (ipc.Message, error) {
	cases, ok := msg.Payload.([]EvalCase)
	if !ok {
		return ipc.Message{}, fmt.Errorf("expected []EvalCase, got %T", msg.Payload)
	}

	r.mu.Lock()
	r.counter++
	run := &EvalRun{
		ID:        fmt.Sprintf("eval_%d", r.counter),
		Status:    "running",
		Cases:     cases,
		StartedAt: time.Now(),
	}
	r.runs[run.ID] = run
	r.mu.Unlock()

	go r.executeRun(run)

	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: map[string]string{"run_id": run.ID}}, nil
}

func (r *Runner) executeRun(run *EvalRun) {
	logger.Info("eval run started", map[string]any{"run_id": run.ID, "cases": len(run.Cases)})

	var totalScore float64
	var passed int

	for _, evalCase := range run.Cases {
		start := time.Now()

		// Send message to agent
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		resp, err := r.bus.Request(ctx, ipc.Message{
			Source: "eval", Target: "agent_runtime", Topic: "agent.chat",
			Payload: agent.ChatRequest{
				Agent:   evalCase.Agent,
				Tenant:  evalCase.Tenant,
				User:    "eval:" + run.ID,
				Message: evalCase.Input,
			},
		})
		cancel()

		result := EvalResult{
			CaseID:   evalCase.ID,
			CaseName: evalCase.Name,
			Duration: time.Since(start),
		}

		if err != nil {
			result.MatchDetails = "Error: " + err.Error()
			result.ActualOutput = ""
			result.Score = 0
		} else if chatResp, ok := resp.Payload.(agent.ChatResponse); ok {
			result.ActualOutput = chatResp.Content
			result.ToolsUsed = chatResp.ToolsUsed

			// Score the result
			outputScore, scoreErr := scoreOutputErr(evalCase, chatResp.Content)
			toolScore := scoreTools(evalCase, chatResp.ToolsUsed)

			if scoreErr != nil {
				// An invalid expected-output regex is an authoring error, not a
				// failing case — surface it instead of silently scoring 0.
				result.Score = 0
				result.Passed = false
				result.MatchDetails = "scoring error: " + scoreErr.Error()
			} else {
				if len(evalCase.ExpectedTools) > 0 {
					result.Score = (outputScore + toolScore) / 2
				} else {
					result.Score = outputScore
				}

				result.Passed = result.Score >= 0.5
				result.MatchDetails = fmt.Sprintf("output_score=%.2f tool_score=%.2f", outputScore, toolScore)
			}
		}

		if result.Passed {
			passed++
		}
		totalScore += result.Score

		// Guard mutation of the shared *EvalRun: handleStatus reads the same
		// pointer under r.mu concurrently.
		r.mu.Lock()
		run.Results = append(run.Results, result)
		r.mu.Unlock()
	}

	r.mu.Lock()
	if len(run.Cases) > 0 {
		run.TotalScore = totalScore / float64(len(run.Cases))
		run.PassRate = float64(passed) / float64(len(run.Cases)) * 100
	}
	run.Status = "completed"
	run.CompletedAt = time.Now()
	r.mu.Unlock()

	logger.Info("eval run completed", map[string]any{
		"run_id": run.ID, "total_score": run.TotalScore, "pass_rate": run.PassRate,
	})
}

// scoreOutputErr scores output against the case expectation and, for the regex
// match mode, surfaces an invalid-regex error instead of silently scoring 0.
// scoreOutput is kept as a thin float-only wrapper for existing callers/tests.
func scoreOutputErr(evalCase EvalCase, output string) (float64, error) {
	if evalCase.ExpectedOutput == "" {
		return 1.0, nil // no output expectation
	}

	mode := evalCase.MatchMode
	if mode == "" {
		mode = "contains"
	}

	switch mode {
	case "exact":
		if strings.TrimSpace(output) == strings.TrimSpace(evalCase.ExpectedOutput) {
			return 1.0, nil
		}
		return 0.0, nil
	case "contains":
		if strings.Contains(strings.ToLower(output), strings.ToLower(evalCase.ExpectedOutput)) {
			return 1.0, nil
		}
		return 0.0, nil
	case "regex":
		matched, err := regexp.MatchString(evalCase.ExpectedOutput, output)
		if err != nil {
			// Invalid expected-output regex: surface it rather than scoring 0.
			return 0.0, fmt.Errorf("invalid expected_output regex %q: %w", evalCase.ExpectedOutput, err)
		}
		if matched {
			return 1.0, nil
		}
		return 0.0, nil
	default:
		return 0.0, nil
	}
}

func scoreOutput(evalCase EvalCase, output string) float64 {
	score, _ := scoreOutputErr(evalCase, output)
	return score
}

func scoreTools(evalCase EvalCase, toolsUsed []string) float64 {
	if len(evalCase.ExpectedTools) == 0 {
		return 1.0
	}

	usedSet := make(map[string]bool)
	for _, t := range toolsUsed {
		usedSet[t] = true
	}

	matched := 0
	for _, expected := range evalCase.ExpectedTools {
		if usedSet[expected] {
			matched++
		}
	}

	return float64(matched) / float64(len(evalCase.ExpectedTools))
}

func (r *Runner) handleStatus(msg ipc.Message) (ipc.Message, error) {
	runID, _ := msg.Payload.(string)
	r.mu.RLock()
	run, exists := r.runs[runID]
	r.mu.RUnlock()
	if !exists {
		return ipc.Message{}, fmt.Errorf("eval run %q not found", runID)
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: run}, nil
}

func (r *Runner) handleList(msg ipc.Message) (ipc.Message, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var runs []EvalRun
	for _, run := range r.runs {
		runs = append(runs, *run)
	}
	if runs == nil {
		runs = []EvalRun{}
	}
	return ipc.Message{Type: ipc.MessageTypeResponse, Payload: runs}, nil
}
