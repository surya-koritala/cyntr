package scheduler

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestJobHistoryRecording(t *testing.T) {
	s := New("")

	run := JobRun{
		ID: "jr1", JobID: "job1", Status: "success",
		Output: "done", StartedAt: time.Now(), Duration: time.Second,
	}
	s.recordJobRun("job1", run)

	s.mu.RLock()
	runs := s.history["job1"]
	s.mu.RUnlock()

	if len(runs) != 1 {
		t.Fatalf("expected 1 run, got %d", len(runs))
	}
	if runs[0].Status != "success" {
		t.Fatal("expected success status")
	}
}

func TestJobHistoryMaxEntries(t *testing.T) {
	s := New("")

	for i := 0; i < 25; i++ {
		s.recordJobRun("job1", JobRun{ID: "jr", JobID: "job1", Status: "success"})
	}

	s.mu.RLock()
	count := len(s.history["job1"])
	s.mu.RUnlock()

	if count != 20 {
		t.Fatalf("expected max 20 runs, got %d", count)
	}
}

func TestJobPersistence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "jobs.json")

	s := New(path)
	s.jobs["job1"] = &Job{ID: "job1", Name: "Test", Tenant: "t", Agent: "a", Message: "hello", Enabled: true}
	s.saveJobs()

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("jobs file not created: %v", err)
	}

	// Load in new scheduler
	s2 := New(path)
	s2.loadJobs()

	if len(s2.jobs) != 1 {
		t.Fatalf("expected 1 job loaded, got %d", len(s2.jobs))
	}
	if s2.jobs["job1"].Name != "Test" {
		t.Fatalf("expected job name 'Test', got %q", s2.jobs["job1"].Name)
	}
}

func TestJobDependencyCheck(t *testing.T) {
	s := New("")
	now := time.Now()
	s.jobs["dep1"] = &Job{ID: "dep1", Enabled: true, LastRun: now}
	s.jobs["main"] = &Job{ID: "main", DependsOn: []string{"dep1"}, LastRun: now.Add(-time.Hour)}

	if !s.checkDependencies(s.jobs["main"]) {
		t.Fatal("dependency should be satisfied (dep1.LastRun > main.LastRun)")
	}
}

func TestJobDependencyUnsatisfied(t *testing.T) {
	s := New("")
	now := time.Now()
	s.jobs["dep1"] = &Job{ID: "dep1", Enabled: true, LastRun: time.Time{}} // never ran
	s.jobs["main"] = &Job{ID: "main", DependsOn: []string{"dep1"}, LastRun: now}

	if s.checkDependencies(s.jobs["main"]) {
		t.Fatal("dependency should NOT be satisfied (dep1 never ran)")
	}
}

func TestJobNoDependencies(t *testing.T) {
	s := New("")
	job := &Job{ID: "solo"}
	if !s.checkDependencies(job) {
		t.Fatal("job with no deps should always pass")
	}
}
