package workflow

import "time"

// StepType defines what kind of action a step performs.
type StepType string

const (
	StepToolCall   StepType = "tool_call"    // execute a tool
	StepAgentChat  StepType = "agent_chat"   // ask an agent
	StepCondition  StepType = "condition"    // branch based on result
	StepApproval   StepType = "approval"     // wait for human approval
	StepWebhook    StepType = "webhook"      // fire outbound webhook
	StepDelay      StepType = "delay"        // wait for duration
	StepParallel   StepType = "parallel"     // run sub-steps in parallel
	StepLoop       StepType = "loop"         // iterate over items
	StepHumanInput StepType = "human_input"  // wait for user input
)

// Step is a single action in a workflow.
type Step struct {
	ID         string            `json:"id" yaml:"id"`
	Name       string            `json:"name" yaml:"name"`
	Type       StepType          `json:"type" yaml:"type"`
	Config     map[string]string `json:"config" yaml:"config"`           // type-specific config
	OnSuccess  string            `json:"on_success" yaml:"on_success"`   // next step ID on success
	OnFailure  string            `json:"on_failure" yaml:"on_failure"`   // next step ID on failure
	RetryCount int               `json:"retry_count" yaml:"retry_count"` // max retries (0 = no retry)
	Timeout    time.Duration     `json:"timeout" yaml:"timeout"`
	SubSteps   []string          `json:"sub_steps" yaml:"sub_steps"`    // for parallel step type
	LoopOver   string            `json:"loop_over" yaml:"loop_over"`    // for loop step type: comma-separated items
}

// Workflow defines a sequence of steps.
type Workflow struct {
	ID          string `json:"id" yaml:"id"`
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description" yaml:"description"`
	Tenant      string `json:"tenant" yaml:"tenant"`
	StartStep   string `json:"start_step" yaml:"start_step"` // ID of first step
	Steps       []Step `json:"steps" yaml:"steps"`
}

// RunStatus tracks the state of a workflow execution.
type RunStatus string

const (
	RunPending      RunStatus = "pending"
	RunRunning      RunStatus = "running"
	RunCompleted    RunStatus = "completed"
	RunFailed       RunStatus = "failed"
	RunWaiting      RunStatus = "waiting_approval"
	RunWaitingInput RunStatus = "waiting_input"
)

// Run is an instance of a workflow execution.
type Run struct {
	ID          string
	WorkflowID  string
	Tenant      string
	Status      RunStatus
	CurrentStep string
	Results     map[string]StepResult // step ID -> result
	StartedAt   time.Time
	CompletedAt time.Time
	Error       string
}

// StepResult holds the output of a completed step.
type StepResult struct {
	StepID    string
	Status    string // "success", "failure", "skipped"
	Output    string
	Error     string
	Duration  time.Duration
	Timestamp time.Time
}
