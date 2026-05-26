package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/modules/eval"
)

// Exit codes for the eval subcommand. CI systems read these.
const (
	evalExitOK        = 0 // threshold met
	evalExitRegressed = 1 // threshold not met
	evalExitError     = 2 // tool/network/parse error
)

// evalFile is the on-disk shape for eval cases. Either top-level cases with
// per-case agent/tenant, or a wrapper that supplies the agent/tenant defaults.
type evalFile struct {
	Agent  string           `json:"agent,omitempty"`
	Tenant string           `json:"tenant,omitempty"`
	Cases  []eval.EvalCase  `json:"cases,omitempty"`
	// If "cases" is omitted, the file may itself be a JSON array of EvalCase.
	Inline []eval.EvalCase `json:"-"`
}

func runEval(args []string) {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: cyntr eval <path> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  Run evaluation cases against a running Cyntr server and exit non-zero")
		fmt.Fprintln(os.Stderr, "  if the pass rate is below the threshold. Designed for CI.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  <path> may be a single .json file or a directory of .json files.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags:")
		fs.PrintDefaults()
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Exit codes: 0 = passed, 1 = threshold not met, 2 = error")
	}

	url := fs.String("url", envOr("CYNTR_API_URL", "http://localhost:7700"), "Cyntr server URL")
	key := fs.String("key", os.Getenv("CYNTR_API_KEY"), "Cyntr API key (or CYNTR_API_KEY env)")
	agentFlag := fs.String("agent", "", "Default agent for cases that don't specify one")
	tenantFlag := fs.String("tenant", "", "Default tenant for cases that don't specify one")
	format := fs.String("format", "text", "Output format: text, json, junit")
	output := fs.String("output", "", "Write results to file (default: stdout)")
	threshold := fs.Float64("threshold", 100.0, "Minimum pass rate % required (exit 1 below this)")
	timeout := fs.Duration("timeout", 10*time.Minute, "Max wait for run completion")
	poll := fs.Duration("poll", 2*time.Second, "Poll interval while waiting")

	if err := fs.Parse(args); err != nil {
		os.Exit(evalExitError)
	}
	if fs.NArg() < 1 {
		fs.Usage()
		os.Exit(evalExitError)
	}

	cases, err := loadEvalCases(fs.Arg(0), *agentFlag, *tenantFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading cases: %v\n", err)
		os.Exit(evalExitError)
	}
	if len(cases) == 0 {
		fmt.Fprintln(os.Stderr, "error: no eval cases found")
		os.Exit(evalExitError)
	}

	run, err := submitAndWait(*url, *key, *agentFlag, *tenantFlag, cases, *timeout, *poll)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error running evals: %v\n", err)
		os.Exit(evalExitError)
	}

	out, err := formatEvalRun(run, *format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error formatting results: %v\n", err)
		os.Exit(evalExitError)
	}

	if *output == "" {
		fmt.Print(out)
	} else if err := os.WriteFile(*output, []byte(out), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing %s: %v\n", *output, err)
		os.Exit(evalExitError)
	}

	if run.PassRate < *threshold {
		fmt.Fprintf(os.Stderr, "\nFAIL: pass rate %.1f%% < threshold %.1f%%\n", run.PassRate, *threshold)
		os.Exit(evalExitRegressed)
	}
	if *format != "text" || *output != "" {
		// Always print a one-line summary to stderr when stdout is consumed
		// by a machine-readable format or written to a file.
		fmt.Fprintf(os.Stderr, "PASS: %.1f%% (%d/%d)\n", run.PassRate, countPassed(run), len(run.Results))
	}
	os.Exit(evalExitOK)
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

func countPassed(run *eval.EvalRun) int {
	n := 0
	for _, r := range run.Results {
		if r.Passed {
			n++
		}
	}
	return n
}

// loadEvalCases reads cases from either a single .json file or every .json
// file in a directory (non-recursive). Files may be either an evalFile object
// or a top-level array of EvalCase.
func loadEvalCases(path, defaultAgent, defaultTenant string) ([]eval.EvalCase, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	var files []string
	if info.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, fmt.Errorf("read dir %s: %w", path, err)
		}
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
				files = append(files, filepath.Join(path, e.Name()))
			}
		}
		sort.Strings(files)
	} else {
		files = []string{path}
	}

	var all []eval.EvalCase
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		cases, err := parseEvalContent(raw)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", f, err)
		}
		for i := range cases {
			if cases[i].Agent == "" {
				cases[i].Agent = defaultAgent
			}
			if cases[i].Tenant == "" {
				cases[i].Tenant = defaultTenant
			}
		}
		all = append(all, cases...)
	}
	return all, nil
}

// parseEvalContent accepts either an evalFile JSON object or a JSON array of
// EvalCase. This lets users write evals/foo.json as just `[{...}, {...}]` for
// quick cases, or use the full envelope when they need per-file defaults.
func parseEvalContent(raw []byte) ([]eval.EvalCase, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, errors.New("empty file")
	}
	if trimmed[0] == '[' {
		var arr []eval.EvalCase
		if err := json.Unmarshal(raw, &arr); err != nil {
			return nil, err
		}
		return arr, nil
	}
	var f evalFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	for i := range f.Cases {
		if f.Cases[i].Agent == "" {
			f.Cases[i].Agent = f.Agent
		}
		if f.Cases[i].Tenant == "" {
			f.Cases[i].Tenant = f.Tenant
		}
	}
	return f.Cases, nil
}

// submitAndWait POSTs cases to /api/v1/eval/run, then polls /api/v1/eval/runs/{id}
// until the run completes or the timeout expires.
func submitAndWait(serverURL, apiKey, agentFlag, tenantFlag string, cases []eval.EvalCase, timeout, poll time.Duration) (*eval.EvalRun, error) {
	body, _ := json.Marshal(map[string]any{
		"agent":  agentFlag,
		"tenant": tenantFlag,
		"cases":  cases,
	})
	req, err := http.NewRequest("POST", strings.TrimRight(serverURL, "/")+"/api/v1/eval/run", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("submit: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("submit failed: HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Response envelope: {"data": {"run_id": "..."}, ...}
	var submitResp struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(respBody, &submitResp); err != nil {
		return nil, fmt.Errorf("parse submit response: %w (body: %s)", err, string(respBody))
	}
	runID := submitResp.Data["run_id"]
	if runID == "" {
		return nil, fmt.Errorf("server did not return a run_id (body: %s)", string(respBody))
	}

	deadline := time.Now().Add(timeout)
	statusURL := strings.TrimRight(serverURL, "/") + "/api/v1/eval/runs/" + runID
	for {
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for run %s to complete", runID)
		}
		req, _ := http.NewRequest("GET", statusURL, nil)
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("status: %w", err)
		}
		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("status failed: HTTP %d: %s", resp.StatusCode, string(bodyBytes))
		}
		var statusResp struct {
			Data eval.EvalRun `json:"data"`
		}
		if err := json.Unmarshal(bodyBytes, &statusResp); err != nil {
			return nil, fmt.Errorf("parse status response: %w", err)
		}
		if statusResp.Data.Status == "completed" || statusResp.Data.Status == "failed" {
			return &statusResp.Data, nil
		}
		time.Sleep(poll)
	}
}

func formatEvalRun(run *eval.EvalRun, format string) (string, error) {
	switch format {
	case "text":
		return formatEvalText(run), nil
	case "json":
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		if err := enc.Encode(run); err != nil {
			return "", err
		}
		return buf.String(), nil
	case "junit":
		return formatEvalJUnit(run)
	default:
		return "", fmt.Errorf("unknown format %q (want text, json, junit)", format)
	}
}

func formatEvalText(run *eval.EvalRun) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Cyntr Eval — %s\n", run.ID)
	fmt.Fprintf(&b, "Cases: %d  Pass rate: %.1f%%  Avg score: %.2f\n\n",
		len(run.Results), run.PassRate, run.TotalScore)
	for _, r := range run.Results {
		mark := "PASS"
		if !r.Passed {
			mark = "FAIL"
		}
		name := r.CaseName
		if name == "" {
			name = r.CaseID
		}
		fmt.Fprintf(&b, "  [%s] %s (%.0fms)  %s\n", mark, name, float64(r.Duration)/float64(time.Millisecond), r.MatchDetails)
		if !r.Passed && r.ActualOutput != "" {
			snippet := r.ActualOutput
			if len(snippet) > 200 {
				snippet = snippet[:200] + "..."
			}
			fmt.Fprintf(&b, "         got: %s\n", strings.ReplaceAll(snippet, "\n", " "))
		}
	}
	return b.String()
}

// JUnit XML structures. Only the bits CI parsers actually read.
type junitTestsuite struct {
	XMLName  xml.Name        `xml:"testsuite"`
	Name     string          `xml:"name,attr"`
	Tests    int             `xml:"tests,attr"`
	Failures int             `xml:"failures,attr"`
	Time     float64         `xml:"time,attr"`
	Cases    []junitTestcase `xml:"testcase"`
}

type junitTestcase struct {
	Name      string        `xml:"name,attr"`
	ClassName string        `xml:"classname,attr"`
	Time      float64       `xml:"time,attr"`
	Failure   *junitFailure `xml:"failure,omitempty"`
}

type junitFailure struct {
	Message string `xml:"message,attr"`
	Body    string `xml:",chardata"`
}

func formatEvalJUnit(run *eval.EvalRun) (string, error) {
	ts := junitTestsuite{
		Name:  "cyntr.eval." + run.ID,
		Tests: len(run.Results),
	}
	var totalSec float64
	for _, r := range run.Results {
		c := junitTestcase{
			Name:      nameOrID(r),
			ClassName: "cyntr.eval",
			Time:      r.Duration.Seconds(),
		}
		totalSec += c.Time
		if !r.Passed {
			ts.Failures++
			body := r.MatchDetails
			if r.ActualOutput != "" {
				body += "\nactual: " + r.ActualOutput
			}
			c.Failure = &junitFailure{
				Message: r.MatchDetails,
				Body:    body,
			}
		}
		ts.Cases = append(ts.Cases, c)
	}
	ts.Time = totalSec

	var buf bytes.Buffer
	buf.WriteString(xml.Header)
	enc := xml.NewEncoder(&buf)
	enc.Indent("", "  ")
	if err := enc.Encode(ts); err != nil {
		return "", err
	}
	if err := enc.Flush(); err != nil {
		return "", err
	}
	buf.WriteString("\n")
	return buf.String(), nil
}

func nameOrID(r eval.EvalResult) string {
	if r.CaseName != "" {
		return r.CaseName
	}
	return r.CaseID
}
