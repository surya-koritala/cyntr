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
	LastRun       time.Time     `json:"last_run"`
	NextRun       time.Time     `json:"next_run"`
	Enabled       bool          `json:"enabled"`
	DestChannel   string        `json:"dest_channel"`    // channel adapter name for report delivery
	DestChannelID string        `json:"dest_channel_id"` // platform channel ID for report delivery
}
