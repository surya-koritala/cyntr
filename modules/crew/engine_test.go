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

func TestCrewTypes(t *testing.T) {
	crew := Crew{
		ID: "c1", Name: "test-crew", Mode: "pipeline", Tenant: "demo",
		Members: []CrewMember{
			{Agent: "researcher", Role: "Research", Goal: "Find facts"},
			{Agent: "writer", Role: "Write", Goal: "Write article"},
		},
	}
	if len(crew.Members) != 2 {
		t.Fatalf("expected 2 members, got %d", len(crew.Members))
	}
	if crew.Mode != "pipeline" {
		t.Fatalf("expected pipeline mode, got %q", crew.Mode)
	}
}

func TestCrewRunInit(t *testing.T) {
	run := CrewRun{
		ID:      "run1",
		CrewID:  "c1",
		Status:  "pending",
		Input:   "Write about AI",
		Results: make(map[string]string),
	}
	if run.Status != "pending" {
		t.Fatal("expected pending status")
	}
	if run.Results == nil {
		t.Fatal("results should be initialized")
	}
}

func TestCrewMemberFields(t *testing.T) {
	m := CrewMember{Agent: "bot", Role: "Researcher", Goal: "Find 5 facts"}
	if m.Agent != "bot" || m.Role != "Researcher" || m.Goal != "Find 5 facts" {
		t.Fatal("fields not set correctly")
	}
}
