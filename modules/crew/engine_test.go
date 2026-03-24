package crew

import "testing"

func TestEngineName(t *testing.T) {
	e := New()
	if e.Name() != "crew" {
		t.Fatal("wrong name")
	}
}

func TestEngineDependencies(t *testing.T) {
	e := New()
	if len(e.Dependencies()) != 1 || e.Dependencies()[0] != "agent_runtime" {
		t.Fatal("expected [agent_runtime]")
	}
}

func TestCrewRunStatusFields(t *testing.T) {
	run := CrewRun{ID: "test", Status: "pending", Results: make(map[string]string)}
	if run.ID != "test" || run.Status != "pending" {
		t.Fatal("fields not set")
	}
}
