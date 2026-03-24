package crew

import "time"

// Crew defines a group of agents that collaborate on a task.
type Crew struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Mode    string       `json:"mode"` // "pipeline", "parallel", "sequential"
	Members []CrewMember `json:"members"`
	Tenant  string       `json:"tenant"`
}

// CrewMember is an agent assigned to a crew with a specific role.
type CrewMember struct {
	Agent string `json:"agent"` // agent name
	Role  string `json:"role"`  // what this agent does in the crew
	Goal  string `json:"goal"`  // specific objective for this agent
}

// CrewRun tracks the execution of a crew task.
type CrewRun struct {
	ID          string            `json:"id"`
	CrewID      string            `json:"crew_id"`
	Status      string            `json:"status"` // "pending", "running", "completed", "failed"
	Input       string            `json:"input"`  // initial task/message
	Results     map[string]string `json:"results"`  // agent name -> output
	Output      string            `json:"output"`   // final aggregated output
	StartedAt   time.Time         `json:"started_at"`
	CompletedAt time.Time         `json:"completed_at"`
	Error       string            `json:"error,omitempty"`
}
