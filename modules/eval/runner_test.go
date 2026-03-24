package eval

import "testing"

func TestRunnerName(t *testing.T) {
	r := New()
	if r.Name() != "eval" {
		t.Fatal("wrong name")
	}
}

func TestScoreOutputContains(t *testing.T) {
	c := EvalCase{ExpectedOutput: "hello", MatchMode: "contains"}
	if scoreOutput(c, "say hello world") != 1.0 {
		t.Fatal("should match contains")
	}
	if scoreOutput(c, "goodbye") != 0.0 {
		t.Fatal("should not match")
	}
}

func TestScoreOutputExact(t *testing.T) {
	c := EvalCase{ExpectedOutput: "Four", MatchMode: "exact"}
	if scoreOutput(c, "Four") != 1.0 {
		t.Fatal("should match exact")
	}
	if scoreOutput(c, "Four.") != 0.0 {
		t.Fatal("should not match with period")
	}
}

func TestScoreOutputRegex(t *testing.T) {
	c := EvalCase{ExpectedOutput: `\d+ instances`, MatchMode: "regex"}
	if scoreOutput(c, "Found 5 instances running") != 1.0 {
		t.Fatal("should match regex")
	}
	if scoreOutput(c, "no instances") != 0.0 {
		t.Fatal("should not match")
	}
}

func TestScoreOutputEmpty(t *testing.T) {
	c := EvalCase{ExpectedOutput: ""}
	if scoreOutput(c, "anything") != 1.0 {
		t.Fatal("empty expected should always score 1.0")
	}
}

func TestScoreToolsAllMatched(t *testing.T) {
	c := EvalCase{ExpectedTools: []string{"shell_exec", "file_read"}}
	score := scoreTools(c, []string{"shell_exec", "file_read", "http_request"})
	if score != 1.0 {
		t.Fatalf("expected 1.0, got %f", score)
	}
}

func TestScoreToolsPartial(t *testing.T) {
	c := EvalCase{ExpectedTools: []string{"shell_exec", "file_read"}}
	score := scoreTools(c, []string{"shell_exec"})
	if score != 0.5 {
		t.Fatalf("expected 0.5, got %f", score)
	}
}

func TestScoreToolsNone(t *testing.T) {
	c := EvalCase{ExpectedTools: []string{"shell_exec"}}
	score := scoreTools(c, []string{"http_request"})
	if score != 0.0 {
		t.Fatalf("expected 0.0, got %f", score)
	}
}

func TestScoreToolsEmpty(t *testing.T) {
	c := EvalCase{ExpectedTools: []string{}}
	if scoreTools(c, []string{"anything"}) != 1.0 {
		t.Fatal("empty expected tools should score 1.0")
	}
}

func TestEvalResultFields(t *testing.T) {
	r := EvalResult{CaseID: "c1", Passed: true, Score: 0.85}
	if !r.Passed || r.Score != 0.85 {
		t.Fatal("fields not set")
	}
}
