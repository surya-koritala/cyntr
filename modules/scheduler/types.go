package scheduler

import "time"

// Job represents a scheduled task.
type Job struct {
	ID       string
	Name     string
	Tenant   string
	Agent    string
	Message  string        // message to send to the agent
	Interval time.Duration // how often to run
	LastRun  time.Time
	NextRun  time.Time
	Enabled  bool
}
