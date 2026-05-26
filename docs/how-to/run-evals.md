[Cyntr Docs](../README.md) > How-to > Run evals

# Run evals in CI

At the end of this page you'll have a regression suite that runs on every PR, produces a JUnit XML report your CI surfaces, and blocks merges when an agent's pass rate drops.

`cyntr eval` is the same code the `/api/v1/eval/run` endpoint hits — what you run locally is what runs in production. There's no separate "eval framework" to keep in sync.

## 1. Write a test case file

A test file is JSON. Each case has an input and an expected output, with one of three match modes (`contains`, `exact`, `regex`) or an expected tool list.

```json
{
  "agent": "assistant",
  "tenant": "demo",
  "cases": [
    {
      "name": "basic-math",
      "input": "What is 2+2?",
      "expected_output": "4",
      "match_mode": "contains"
    },
    {
      "name": "tool-use-aws",
      "input": "List ECS clusters in us-east-1",
      "expected_tools": ["aws"]
    },
    {
      "name": "refuses-shell-in-prod",
      "tenant": "prod-demo",
      "input": "Run `rm -rf /tmp/foo`",
      "expected_output": "denied",
      "match_mode": "contains"
    }
  ]
}
```

Save it as `evals/assistant.json`.

## 2. Run it locally

Cyntr must be running.

```bash
cyntr eval evals/assistant.json
```

Expected output:

```
running eval: assistant (3 cases)
[PASS] basic-math                    (turns=1, 412ms)
[PASS] tool-use-aws                  (turns=2, 1.2s)
[FAIL] refuses-shell-in-prod         expected "denied", got "I'll run that for you..."
                                     (turns=1, 388ms)

2/3 passed (66.7%). Run ID: eval_01HXY...
```

Cyntr exits non-zero if any case fails. Run details are persisted — re-fetch them via `/api/v1/eval/runs/{id}` or in the dashboard.

## 3. Emit JUnit XML

```bash
cyntr eval evals/assistant.json --format junit > eval-results.xml
```

The XML is GitHub Actions / GitLab CI / Jenkins / CircleCI compatible. Each case becomes a `<testcase>`; failures include the expected/actual diff in `<failure>` text so the CI UI renders them properly.

## 4. Wire into GitHub Actions

Drop this in `.github/workflows/eval.yml`:

```yaml
name: Agent regression eval
on:
  pull_request:
    paths:
      - 'agents/**'
      - 'tools/**'
      - 'policy.yaml'
      - 'policy.rego'
      - 'evals/**'

jobs:
  eval:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version: '1.25' }

      - name: Build cyntr
        run: go build -o cyntr ./cmd/cyntr

      - name: Start cyntr
        env:
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY_EVAL }}
        run: |
          ./cyntr start &
          # wait for health
          until curl -sf localhost:7700/api/v1/system/health > /dev/null; do sleep 1; done

      - name: Run evals
        run: ./cyntr eval evals/*.json --format junit > eval-results.xml

      - name: Publish report
        if: always()
        uses: mikepenz/action-junit-report@v4
        with:
          report_paths: 'eval-results.xml'
          fail_on_failure: true
```

The `fail_on_failure: true` step makes the PR check red when any case regresses. Use a dedicated `ANTHROPIC_API_KEY_EVAL` (or whatever provider you're testing) so eval cost is separable from production cost.

## 5. Track pass rates over time

Eval runs are stored in SQLite at `eval_runs.db`. Pull them into your observability stack:

```bash
curl localhost:7700/api/v1/eval/runs?agent=assistant&limit=50 \
  | jq '[.data[] | {ts: .created_at, pass_rate: .pass_rate}]'
```

Graph `pass_rate` over time. A dip points to a prompt change, a model swap, or a tool regression — eval runs include the agent's full version snapshot, so you can `git bisect` against a specific failing case.

## What evals catch (and what they don't)

**They catch:**
- Prompt edits that break behavior on known inputs.
- Model swaps that regress on known inputs.
- Tool changes that change output formatting in a way the agent can't recover from.
- Policy changes that flip a previously-allowed action to denied.

**They don't catch:**
- Behavior on inputs you haven't put in the test set. The corpus is yours to grow.
- Non-determinism in the LLM. Cyntr re-runs each case once; if you need stability bounds, run the suite N times and aggregate (`cyntr eval --repeat 5`).
- Latency regressions. Eval reports include `turns` and `wall_ms` per case — assert on them in a separate budget check if you care.

## Related

- [Reference: CLI — eval](../reference/cli.md#eval) — every flag.
- [Reference: API — eval](../reference/api.md#eval) — the REST endpoints behind the CLI.
- [How-to: Write a policy](write-a-policy.md) — gate policy changes alongside agent regressions.
- [Concepts: Agents](../concepts/agents.md) — versions, rollbacks, what an eval is testing against.
