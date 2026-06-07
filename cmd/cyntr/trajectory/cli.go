package trajectory

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cyntr-dev/cyntr/kernel/jobs"
	"github.com/cyntr-dev/cyntr/modules/eval"
)

// Run is the `cyntr trajectory <run|compress>` entrypoint. It is wired from
// cmd/cyntr/main.go's command switch (see the integration snippet) so the
// integrator owns main.go but this subtree owns its own flag parsing.
func Run(args []string) int {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: cyntr trajectory <run|compress> [flags]")
		return 2
	}
	switch args[0] {
	case "run":
		return runBatch(args[1:])
	case "compress":
		return runCompress(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown trajectory command: %s\n", args[0])
		return 2
	}
}

// runBatch implements `cyntr trajectory run --suite X --n N`. It fans N runs
// out over a local durable jobs queue, each going through the running server's
// isolated agent.chat path, and writes the batch as JSONL.
func runBatch(args []string) int {
	fs := flag.NewFlagSet("trajectory run", flag.ContinueOnError)
	suite := fs.String("suite", "", "suite/batch label to tag every trajectory")
	n := fs.Int("n", 1, "number of runs to fan out")
	tenant := fs.String("tenant", "", "tenant (required)")
	agentName := fs.String("agent", "", "agent to run (required)")
	user := fs.String("user", "trajectory-run", "user to attribute runs to")
	prompt := fs.String("prompt", "", "prompt to send on each run (required)")
	url := fs.String("url", envOr("CYNTR_API_URL", "http://localhost:7700"), "Cyntr server URL")
	key := fs.String("key", os.Getenv("CYNTR_API_KEY"), "Cyntr API key (or CYNTR_API_KEY env)")
	out := fs.String("output", "", "write JSONL here (default: stdout)")
	dbPath := fs.String("queue-db", "trajectory_jobs.db", "path for the durable jobs queue")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *tenant == "" || *agentName == "" || *prompt == "" {
		fmt.Fprintln(os.Stderr, "trajectory run: --tenant, --agent and --prompt are required")
		return 2
	}

	queue, err := jobs.NewQueue(*dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trajectory run: open queue: %v\n", err)
		return 1
	}
	defer queue.Close()

	runner := NewRunner(queue, httpIsolatedRun(*url, *key), nil)
	base := Sample{Tenant: *tenant, Agent: *agentName, User: *user, Suite: *suite, Prompt: *prompt}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	trajs, err := runner.Run(ctx, base, *n)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trajectory run: %v\n", err)
		// Still export whatever succeeded.
	}

	var buf bytes.Buffer
	if werr := ExportJSONL(&buf, trajs); werr != nil {
		fmt.Fprintf(os.Stderr, "trajectory run: export: %v\n", werr)
		return 1
	}
	if *out == "" {
		fmt.Print(buf.String())
	} else if werr := os.WriteFile(*out, buf.Bytes(), 0644); werr != nil {
		fmt.Fprintf(os.Stderr, "trajectory run: write %s: %v\n", *out, werr)
		return 1
	}
	fmt.Fprintf(os.Stderr, "trajectory run: %d/%d runs captured\n", len(trajs), *n)
	if err != nil {
		return 1
	}
	return 0
}

// runCompress implements `cyntr trajectory compress`: an offline stream-to-
// stream transform over G28 JSONL output.
func runCompress(args []string) int {
	fs := flag.NewFlagSet("trajectory compress", flag.ContinueOnError)
	in := fs.String("input", "", "raw trajectory JSONL (default: stdin)")
	out := fs.String("output", "", "compact JSONL (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var r io.Reader = os.Stdin
	if *in != "" {
		f, err := os.Open(*in)
		if err != nil {
			fmt.Fprintf(os.Stderr, "trajectory compress: open %s: %v\n", *in, err)
			return 1
		}
		defer f.Close()
		r = f
	}

	var buf bytes.Buffer
	count, err := eval.CompressStream(r, &buf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "trajectory compress: %v\n", err)
		return 1
	}
	if *out == "" {
		fmt.Print(buf.String())
	} else if werr := os.WriteFile(*out, buf.Bytes(), 0644); werr != nil {
		fmt.Fprintf(os.Stderr, "trajectory compress: write %s: %v\n", *out, werr)
		return 1
	}
	fmt.Fprintf(os.Stderr, "trajectory compress: %d records\n", count)
	return 0
}

// httpIsolatedRun returns an IsolatedRun that drives a sample through the
// running server's isolated agent.chat endpoint. The server spawns a fresh,
// tenant-scoped session per call (the same isolated-subagent contract used by
// delegate/orchestrate), and returns the final content plus the ordered tool
// decision sequence, from which we assemble the trajectory.
func httpIsolatedRun(serverURL, apiKey string) IsolatedRun {
	client := &http.Client{Timeout: 5 * time.Minute}
	base := strings.TrimRight(serverURL, "/")
	return func(ctx context.Context, s Sample) (eval.Trajectory, error) {
		body, _ := json.Marshal(map[string]string{"message": s.Prompt, "user": s.User})
		endpoint := fmt.Sprintf("%s/api/v1/tenants/%s/agents/%s/chat", base, s.Tenant, s.Agent)
		req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(body))
		if err != nil {
			return eval.Trajectory{}, err
		}
		req.Header.Set("Content-Type", "application/json")
		if apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+apiKey)
		}
		resp, err := client.Do(req)
		if err != nil {
			return eval.Trajectory{}, fmt.Errorf("chat request: %w", err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			return eval.Trajectory{}, fmt.Errorf("chat HTTP %d: %s", resp.StatusCode, string(raw))
		}
		var env struct {
			Data struct {
				Agent     string   `json:"agent"`
				Content   string   `json:"content"`
				ToolsUsed []string `json:"tools_used"`
			} `json:"data"`
		}
		if err := json.Unmarshal(raw, &env); err != nil {
			return eval.Trajectory{}, fmt.Errorf("parse chat response: %w (body: %s)", err, string(raw))
		}
		steps := make([]eval.TrajectoryStep, 0, len(env.Data.ToolsUsed))
		for i, tool := range env.Data.ToolsUsed {
			steps = append(steps, eval.TrajectoryStep{Index: i, Tool: tool})
		}
		return eval.Trajectory{
			Schema:    eval.TrajectorySchemaRaw,
			Tenant:    s.Tenant,
			User:      s.User,
			Agent:     s.Agent,
			Suite:     s.Suite,
			RunID:     s.RunID,
			Prompt:    s.Prompt,
			Steps:     steps,
			Output:    env.Data.Content,
			Outcome:   "ok",
			ToolCalls: len(env.Data.ToolsUsed),
			Turns:     1,
			CreatedAt: time.Now(),
		}, nil
	}
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
