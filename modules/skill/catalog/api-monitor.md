---
name: api-monitor
description: Monitor API endpoint health, latency, and schema consistency
version: 1.0.0
author: cyntr
tools:
  - name: http_request
  - name: shell_exec
  - name: file_read
  - name: file_write
---

# API Health Monitor

Check the health and performance of a set of API endpoints, measure response
times, validate status codes and response schemas, and generate a health report.

## Prerequisites

- The user must provide a list of API endpoints to monitor. Each endpoint
  should include at minimum: URL and expected HTTP status code.
- Optionally, the user may provide: HTTP method (default GET), request headers,
  request body, expected response schema, and baseline latency.
- If a configuration file is provided (JSON format), read it with `file_read`.

## Input Format

Endpoints can be specified as a JSON array:

```json
[
  {
    "name": "User Service Health",
    "url": "https://api.example.com/health",
    "method": "GET",
    "expected_status": 200,
    "timeout_ms": 5000,
    "headers": { "Authorization": "Bearer <token>" },
    "expected_body_contains": "\"status\":\"ok\"",
    "baseline_latency_ms": 200
  }
]
```

If the user provides endpoints informally (e.g., "check api.example.com/health
and api.example.com/users"), construct the configuration with sensible defaults:
method=GET, expected_status=200, timeout_ms=5000, no auth headers.

## Step 1: Validate Endpoint List

Parse the endpoint list. For each endpoint, verify:
- URL is well-formed (starts with `http://` or `https://`)
- Method is valid (`GET`, `POST`, `PUT`, `PATCH`, `DELETE`, `HEAD`, `OPTIONS`)
- Expected status is a valid HTTP status code (100-599)

If any endpoint is invalid, report the issue and skip that endpoint.

## Step 2: Execute Health Checks

For each endpoint, execute the HTTP request using `http_request` or via shell:

```
curl -s -o /dev/null -w "%{http_code} %{time_total} %{size_download}" \
  -X <method> \
  -H "Content-Type: application/json" \
  <additional_headers> \
  --max-time <timeout_s> \
  "<url>"
```

Capture for each request:
- **HTTP status code** returned
- **Response time** in milliseconds
- **Response body size** in bytes
- **Response body** (first 2000 characters for schema validation)
- **Error** if the request failed entirely (timeout, DNS failure, connection
  refused)

Run checks sequentially to avoid overwhelming target services. Pause 500ms
between requests if checking more than 10 endpoints.

## Step 3: Status Code Validation

For each endpoint, compare the actual status code to the expected status code.

Classify results:
- **Pass**: Actual matches expected
- **Fail**: Actual does not match expected
- **Error**: Request failed entirely (timeout, connection error)

For failures, categorize by status code range:
- `4xx`: Client error (likely a configuration issue on our side: bad URL, auth)
- `5xx`: Server error (the service has a problem)
- `3xx`: Redirect (may be expected or may indicate a configuration change)

## Step 4: Latency Analysis

For each endpoint, evaluate response time:

- **Fast** (green): < 500ms or < baseline if provided
- **Acceptable** (yellow): 500ms-2000ms or within 2x baseline
- **Slow** (red): > 2000ms or > 3x baseline

If baselines are provided, compute:
- Absolute difference from baseline
- Percentage change from baseline
- Flag any endpoint more than 50% slower than baseline

Compute aggregate latency statistics across all endpoints:
- Mean response time
- Median response time (sort and take middle value)
- P95 response time (95th percentile)
- Slowest endpoint

## Step 5: Response Body Validation

For each endpoint where `expected_body_contains` is specified, check whether the
response body contains the expected string. Report pass or fail.

If the user provided an expected JSON schema, validate the response structure:
- Check that all expected top-level keys are present
- Check that value types match expectations (string, number, array, object)
- Flag any unexpected keys that were not in the previous baseline (potential
  schema change)

For JSON responses, also check:
- Is the response valid JSON? If not, flag as a critical issue.
- Has the response structure changed since the last known-good baseline?

## Step 6: Availability Calculation

If the user requests multiple rounds of checks (e.g., "check every 5 minutes
for 1 hour"), track results over time and compute:

- **Uptime percentage**: (successful_checks / total_checks) * 100
- **Mean time between failures**: Average duration between consecutive failures
- **Longest outage**: Longest streak of consecutive failures

For single-check runs, report the instantaneous availability (pass/fail) for
each endpoint.

## Step 7: Error Pattern Detection

Analyze any failures for patterns:
- Are all failures on the same host? (Host-level issue)
- Are all failures the same status code? (Systematic error)
- Are all failures timeouts? (Network or load issue)
- Are failures correlated with response size? (Payload issue)

If `shell_exec` is available and the user has access, run DNS resolution check:

```
dig +short <hostname>
```

And connectivity check:

```
curl -s -o /dev/null -w "%{http_code}" --max-time 5 <base_url>
```

## Step 8: Generate Report

Compile all findings into this format:

```
# API Health Report
**Generated:** <current date and time>
**Endpoints checked:** <count>
**Overall status:** <All Healthy | Degraded | Critical>

## Summary
| Status | Count |
|--------|-------|
| Pass   | <n>   |
| Fail   | <n>   |
| Error  | <n>   |

## Endpoint Details
| # | Name | URL | Status | Code | Latency | Size | Result |
|---|------|-----|--------|------|---------|------|--------|
<rows>

## Latency Statistics
- Mean: <x>ms
- Median: <x>ms
- P95: <x>ms
- Slowest: <endpoint name> at <x>ms

## Issues Found

### Critical
<Endpoints returning 5xx, connection failures, invalid JSON responses>

### Warning
<Slow responses, status code mismatches, schema changes>

### Info
<Minor latency increases, new response fields>

## Recommendations
1. <Specific actionable recommendation>
2. <Next recommendation>
...
```

Determine overall status:
- **All Healthy**: Every endpoint passed with acceptable latency
- **Degraded**: Some endpoints slow or returning unexpected (non-5xx) codes
- **Critical**: Any endpoint returning 5xx, timing out, or unreachable

If the user requested a file output, write the report using `file_write`.
Otherwise, return the report directly.
