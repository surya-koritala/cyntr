package eval

import "time"

// EvalCase defines a single test case for agent evaluation.
type EvalCase struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Agent          string   `json:"agent"`
	Tenant         string   `json:"tenant"`
	Input          string   `json:"input"`           // message to send
	ExpectedOutput string   `json:"expected_output"` // substring that must appear in response
	ExpectedTools  []string `json:"expected_tools"`  // tools that should be used
	MatchMode      string   `json:"match_mode"`      // "contains", "exact", "regex"
}

// EvalResult is the outcome of running a single eval case.
type EvalResult struct {
	CaseID       string        `json:"case_id"`
	CaseName     string        `json:"case_name"`
	Passed       bool          `json:"passed"`
	Score        float64       `json:"score"`        // 0.0 to 1.0
	ActualOutput string        `json:"actual_output"`
	ToolsUsed    []string      `json:"tools_used"`
	MatchDetails string        `json:"match_details"` // why it passed/failed
	Duration     time.Duration `json:"duration"`
}

// EvalRun is a complete evaluation run with multiple cases.
type EvalRun struct {
	ID          string       `json:"id"`
	Status      string       `json:"status"`      // "running", "completed", "failed"
	Cases       []EvalCase   `json:"cases"`
	Results     []EvalResult `json:"results"`
	TotalScore  float64      `json:"total_score"`  // average score
	PassRate    float64      `json:"pass_rate"`     // percentage passed
	StartedAt   time.Time    `json:"started_at"`
	CompletedAt time.Time    `json:"completed_at"`
}
