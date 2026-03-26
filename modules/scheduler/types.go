package scheduler

import "time"

// Job represents a scheduled task.
type Job struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Tenant        string        `json:"tenant"`
	Agent         string        `json:"agent"`
	Message       string        `json:"message"`         // message to send to the agent
	Interval      time.Duration `json:"interval"`        // how often to run
	CronExpr      string        `json:"cron_expr"`       // 5-field cron expression (optional, overrides Interval)
	LastRun       time.Time     `json:"last_run"`
	NextRun       time.Time     `json:"next_run"`
	Enabled       bool          `json:"enabled"`
	DestChannel   string        `json:"dest_channel"`    // channel adapter name for report delivery
	DestChannelID string        `json:"dest_channel_id"` // platform channel ID for report delivery
	Condition     *JobCondition `json:"condition,omitempty"`
	DependsOn     []string      `json:"depends_on,omitempty"`
	ReportMode    bool          `json:"report_mode"`          // format output as structured report
	ReportTitle   string        `json:"report_title"`         // report header (default: "Scheduled Report: {name}")
}

// JobCondition defines a condition that must be met for a job to produce output.
type JobCondition struct {
	Type    string `json:"type"`    // "output_changed", "output_matches"
	Pattern string `json:"pattern"` // regex for output_matches
}

// JobRun records the result of a single job execution.
type JobRun struct {
	ID        string        `json:"id"`
	JobID     string        `json:"job_id"`
	Status    string        `json:"status"` // "success", "failure"
	Output    string        `json:"output"`
	Error     string        `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
	StartedAt time.Time     `json:"started_at"`
}
