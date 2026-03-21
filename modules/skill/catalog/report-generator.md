---
name: report-generator
description: Generate structured reports from databases, APIs, files, and shell commands
version: 1.0.0
author: cyntr
tools:
  - name: database_query
  - name: http_request
  - name: file_read
  - name: file_write
  - name: shell_exec
---

# Multi-Source Report Generator

Pull data from multiple sources (databases, APIs, files, shell commands),
aggregate key metrics, and produce a formatted markdown report.

## Prerequisites

- The user must specify the report type: `daily`, `weekly`, or `monthly`.
- The user must specify at least one data source.
- The user may optionally provide a report template or previous report for
  comparison.

## Step 1: Determine Report Parameters

Based on the report type, set the time window:

- **Daily**: Last 24 hours. Compare to previous day.
- **Weekly**: Last 7 days. Compare to previous 7-day period.
- **Monthly**: Last 30 days. Compare to previous 30-day period.

Set date variables:

```
REPORT_START=$(date -v-1d +%Y-%m-%d 2>/dev/null || date -d "1 day ago" +%Y-%m-%d)
REPORT_END=$(date +%Y-%m-%d)
PREVIOUS_START=$(date -v-2d +%Y-%m-%d 2>/dev/null || date -d "2 days ago" +%Y-%m-%d)
PREVIOUS_END=$REPORT_START
```

Adjust these based on the actual report type (replace `-1d` with `-7d` or
`-30d` as appropriate).

## Step 2: Collect Data from Each Source

Process each data source the user specified. Supported source types:

### Database Source

Use `database_query` to execute SQL queries. The user should provide either
specific queries or table names to summarize.

For table summaries:
```sql
SELECT COUNT(*) AS total_rows FROM <table> WHERE created_at >= '<start_date>';
SELECT COUNT(*) AS previous_rows FROM <table> WHERE created_at >= '<prev_start>' AND created_at < '<start_date>';
```

For custom metrics:
```sql
-- Execute whatever queries the user specifies
-- Store results with metric names for the report
```

Record each metric as a key-value pair: `{ name, current_value, previous_value, unit }`.

### API Source

Use `http_request` to call API endpoints. The user should provide URLs and
specify which fields to extract.

```
curl -s -H "Authorization: Bearer <token>" "<api_url>"
```

Parse the JSON response and extract the specified fields. If the API supports
date filtering, pass the report time window as query parameters.

### File Source

Use `file_read` to read data files (CSV, JSON, log files).

For CSV files:
- Count rows within the date range
- Sum or average numeric columns
- Count unique values in categorical columns

For log files:
- Count lines matching specific patterns (errors, warnings)
- Extract timestamps and filter to the report window

```
grep -c "ERROR" <logfile>
grep -c "WARN" <logfile>
awk '/ERROR/ { print }' <logfile> | tail -5
```

### Shell Command Source

Use `shell_exec` to run arbitrary commands the user specifies. Common examples:

```
# Disk usage
df -h | grep -v tmpfs

# Process count
ps aux | wc -l

# Docker container status
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}"

# Git activity
git log --since="<start_date>" --oneline | wc -l
```

Capture each command's output and extract the relevant metric.

## Step 3: Compute Comparisons

For each metric that has both a current and previous value, compute:

- **Absolute change**: current - previous
- **Percentage change**: ((current - previous) / previous) * 100
- **Direction**: up, down, or flat (within 1% is flat)
- **Trend indicator**: Use text indicators:
  - Increase: `[+12.5%]`
  - Decrease: `[-8.3%]`
  - Flat: `[~0%]`

Flag any metric with a change greater than 25% as "significant."
Flag any metric with a change greater than 50% as "alert-worthy."

## Step 4: Identify Key Takeaways

Analyze the collected metrics to produce 3-5 key takeaways:

1. **Biggest positive change**: The metric with the largest positive percentage
   change.
2. **Biggest negative change**: The metric with the largest negative percentage
   change.
3. **Anomalies**: Any metric that changed by more than 2x its typical variance
   (if historical data is available) or more than 50%.
4. **Trends**: If comparing to multiple previous periods, identify consistent
   upward or downward trends.
5. **Action items**: Metrics that crossed a threshold (e.g., error rate above
   5%, disk usage above 80%).

## Step 5: Format the Report

Structure the report as follows:

```
# <Report Type> Report: <Start Date> to <End Date>
**Generated:** <current datetime>
**Period:** <report type> (<start> to <end>)
**Previous period:** <prev_start> to <prev_end>

## Key Takeaways
- <Takeaway 1 with specific numbers>
- <Takeaway 2>
- <Takeaway 3>

## Metrics Summary

### <Category 1: e.g., Application>
| Metric | Current | Previous | Change |
|--------|---------|----------|--------|
| <name> | <value> | <value>  | <pct>  |

### <Category 2: e.g., Infrastructure>
| Metric | Current | Previous | Change |
|--------|---------|----------|--------|
| <name> | <value> | <value>  | <pct>  |

### <Category 3: e.g., Business>
| Metric | Current | Previous | Change |
|--------|---------|----------|--------|
| <name> | <value> | <value>  | <pct>  |

## Detailed Findings

### <Section per data source or topic>
<Narrative description of findings, with specific numbers and context>

## Alerts
<Any metrics that crossed thresholds or changed significantly>

## Action Items
- [ ] <Specific action with owner if known>
- [ ] <Next action>

---
*Report generated by cyntr/report-generator v1.0.0*
```

## Step 6: Handle Previous Report Comparison

If the user provides a previous report file, read it with `file_read` and:

1. Extract the metrics table from the previous report.
2. Compare each metric's "Current" value from the previous report to the
   current report's "Current" value.
3. Add a "Trend" column showing the direction over multiple periods.
4. Note any metrics that have been consistently declining or growing for 3 or
   more consecutive reports.

## Step 7: Output

If the user specified an output file path, write the report using `file_write`.
Suggest a filename based on the pattern:
`report-<type>-<end_date>.md`

If no output path is specified, return the full report as the response.

If the user requested a specific format other than markdown (e.g., plain text),
adjust formatting:
- Remove markdown table syntax, use tab-separated columns instead
- Remove heading markers (`#`), use uppercase text instead
- Keep the overall structure the same
