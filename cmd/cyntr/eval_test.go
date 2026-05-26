package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cyntr-dev/cyntr/modules/eval"
)

func TestParseEvalContent_Array(t *testing.T) {
	raw := []byte(`[
		{"id":"c1","name":"basic","input":"hi","expected_output":"hello","match_mode":"contains"},
		{"id":"c2","name":"tool","input":"list","expected_tools":["aws"]}
	]`)
	cases, err := parseEvalContent(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(cases))
	}
	if cases[0].ID != "c1" || cases[1].ExpectedTools[0] != "aws" {
		t.Fatalf("parse mismatch: %+v", cases)
	}
}

func TestParseEvalContent_Envelope(t *testing.T) {
	raw := []byte(`{
		"agent":"assistant",
		"tenant":"ops",
		"cases":[
			{"id":"c1","input":"hi","expected_output":"hello"},
			{"id":"c2","tenant":"override","input":"hi"}
		]
	}`)
	cases, err := parseEvalContent(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 2 {
		t.Fatalf("expected 2 cases, got %d", len(cases))
	}
	if cases[0].Agent != "assistant" || cases[0].Tenant != "ops" {
		t.Fatalf("defaults not applied to first case: %+v", cases[0])
	}
	if cases[1].Tenant != "override" {
		t.Fatalf("per-case tenant override lost: %+v", cases[1])
	}
}

func TestParseEvalContent_Empty(t *testing.T) {
	_, err := parseEvalContent([]byte(""))
	if err == nil {
		t.Fatal("expected error on empty input")
	}
}

func TestParseEvalContent_BadJSON(t *testing.T) {
	_, err := parseEvalContent([]byte(`{ not json`))
	if err == nil {
		t.Fatal("expected error on bad JSON")
	}
}

func TestLoadEvalCases_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "case.json")
	os.WriteFile(path, []byte(`[{"id":"x","input":"hi"}]`), 0644)

	cases, err := loadEvalCases(path, "default-agent", "default-tenant")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 1 || cases[0].Agent != "default-agent" || cases[0].Tenant != "default-tenant" {
		t.Fatalf("CLI defaults not applied: %+v", cases)
	}
}

func TestLoadEvalCases_Directory(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "b.json"), []byte(`[{"id":"b1"}]`), 0644)
	os.WriteFile(filepath.Join(dir, "a.json"), []byte(`[{"id":"a1"},{"id":"a2"}]`), 0644)
	os.WriteFile(filepath.Join(dir, "ignore.txt"), []byte(`not eval`), 0644) // should be skipped

	cases, err := loadEvalCases(dir, "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cases) != 3 {
		t.Fatalf("expected 3 cases (a.json's 2 + b.json's 1), got %d: %+v", len(cases), cases)
	}
	// a.json should come before b.json alphabetically.
	if cases[0].ID != "a1" || cases[1].ID != "a2" || cases[2].ID != "b1" {
		t.Fatalf("files not sorted alphabetically: %+v", cases)
	}
}

func TestLoadEvalCases_MissingPath(t *testing.T) {
	_, err := loadEvalCases("/no/such/file.json", "", "")
	if err == nil {
		t.Fatal("expected error for missing path")
	}
}

func fakeRun() *eval.EvalRun {
	return &eval.EvalRun{
		ID:       "eval_1",
		Status:   "completed",
		PassRate: 50.0,
		Results: []eval.EvalResult{
			{CaseID: "c1", CaseName: "passes", Passed: true, Score: 1.0,
				ActualOutput: "yes", MatchDetails: "output_score=1.00", Duration: 200 * time.Millisecond},
			{CaseID: "c2", CaseName: "fails", Passed: false, Score: 0.0,
				ActualOutput: "nope (wrong answer)", MatchDetails: "output_score=0.00", Duration: 150 * time.Millisecond},
		},
	}
}

func TestFormatEvalRun_Text(t *testing.T) {
	out, err := formatEvalRun(fakeRun(), "text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "PASS") || !strings.Contains(out, "FAIL") {
		t.Fatalf("expected both PASS and FAIL markers, got: %s", out)
	}
	if !strings.Contains(out, "got: nope") {
		t.Fatalf("expected failing case to include 'got:' snippet, got: %s", out)
	}
	if !strings.Contains(out, "Pass rate: 50.0%") {
		t.Fatalf("expected pass rate in text: %s", out)
	}
}

func TestFormatEvalRun_JSON(t *testing.T) {
	out, err := formatEvalRun(fakeRun(), "json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"pass_rate": 50`) {
		t.Fatalf("expected pass_rate field, got: %s", out)
	}
}

func TestFormatEvalRun_JUnit(t *testing.T) {
	out, err := formatEvalRun(fakeRun(), "junit")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Parse it back to validate the structure.
	var suite junitTestsuite
	// xml.Header is the first line; strip and decode rest.
	body := out
	if idx := strings.Index(body, "?>"); idx >= 0 {
		body = body[idx+2:]
	}
	if err := xml.Unmarshal([]byte(body), &suite); err != nil {
		t.Fatalf("output is not valid XML: %v\n%s", err, out)
	}
	if suite.Tests != 2 {
		t.Fatalf("expected tests=2, got %d", suite.Tests)
	}
	if suite.Failures != 1 {
		t.Fatalf("expected failures=1, got %d", suite.Failures)
	}
	if len(suite.Cases) != 2 {
		t.Fatalf("expected 2 testcase elements, got %d", len(suite.Cases))
	}
	// The failing case should have a <failure> element with the actual output.
	var failingCase *junitTestcase
	for i := range suite.Cases {
		if suite.Cases[i].Name == "fails" {
			failingCase = &suite.Cases[i]
		}
	}
	if failingCase == nil || failingCase.Failure == nil {
		t.Fatal("failing case missing <failure> element")
	}
	if !strings.Contains(failingCase.Failure.Body, "nope") {
		t.Fatalf("failure body should contain actual output, got: %s", failingCase.Failure.Body)
	}
}

func TestFormatEvalRun_UnknownFormat(t *testing.T) {
	_, err := formatEvalRun(fakeRun(), "xml")
	if err == nil {
		t.Fatal("expected error for unknown format")
	}
}

func TestCountPassed(t *testing.T) {
	if got := countPassed(fakeRun()); got != 1 {
		t.Fatalf("expected 1 passed, got %d", got)
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("CYNTR_TEST_VAR", "from-env")
	if got := envOr("CYNTR_TEST_VAR", "fallback"); got != "from-env" {
		t.Fatalf("expected env value, got %q", got)
	}
	if got := envOr("CYNTR_NOPE_NOPE", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback, got %q", got)
	}
}
