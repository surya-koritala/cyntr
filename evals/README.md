# Eval cases

`cyntr eval <path>` runs the cases in this directory (or a single file) against
a Cyntr server and exits non-zero when the pass rate is below the threshold.
Designed for CI.

## File format

Either a top-level JSON array of cases:

```json
[
  {"id":"c1","input":"What is 2+2?","expected_output":"4","match_mode":"contains"}
]
```

…or an envelope with per-file defaults:

```json
{
  "agent": "assistant",
  "tenant": "ops",
  "cases": [
    {"id":"c1","input":"What is 2+2?","expected_output":"4","match_mode":"contains"},
    {"id":"c2","input":"List S3 buckets","expected_tools":["aws"]}
  ]
}
```

Per-case `agent`/`tenant` override file defaults; CLI `--agent`/`--tenant`
flags override anything missing in the file.

## Match modes

| Mode | What it does |
|---|---|
| `contains` (default) | Pass if `expected_output` appears in the response (case-insensitive) |
| `exact` | Pass on whitespace-trimmed equality |
| `regex` | Pass if `expected_output` (a regex) matches the response |

`expected_tools` is scored separately. When present, the case score averages
output match and tool-match coverage.

## Running

```bash
# Against a local server
cyntr eval ./evals/

# Against a deployed server, JUnit output, fail if pass rate < 90%
cyntr eval ./evals/ \
  --url https://cyntr.example.com \
  --key "$CYNTR_API_KEY" \
  --format junit \
  --output results.xml \
  --threshold 90
```

Exit codes: `0` = passed threshold, `1` = regression, `2` = tool error.
