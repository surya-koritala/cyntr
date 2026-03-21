---
name: csv-analyzer
description: Analyze CSV files for data quality, statistics, and anomaly detection
version: 1.0.0
author: cyntr
tools:
  - name: file_read
  - name: file_write
  - name: shell_exec
---

# CSV Data Analyzer

Perform thorough statistical analysis and data quality assessment on CSV files,
producing a structured report with actionable findings.

## Prerequisites

- The user must provide a file path to a CSV file.
- Verify the file exists using `file_read` on the first few lines.
- If the file is larger than 100,000 lines, inform the user that analysis will
  be performed on a sample and proceed with sampling.

## Step 1: Initial File Assessment

Read the first 20 lines of the file to determine:

```
head -20 <file_path>
```

Determine:
- **Delimiter**: Check for commas, tabs, semicolons, or pipes. Count occurrences
  of each in the first line to auto-detect.
- **Header row**: The first line is assumed to be headers unless all values are
  numeric or the user specifies otherwise.
- **Encoding issues**: Look for BOM characters (`\xEF\xBB\xBF`) or garbled text
  indicating encoding mismatch.

Get the total row count:

```
wc -l < <file_path>
```

Subtract 1 for the header row. Report total data rows.

## Step 2: Column Type Detection

Read the first 100 data rows (after the header). For each column, classify its
type by examining values:

- **Integer**: All values match `^-?[0-9]+$`
- **Float**: All values match `^-?[0-9]*\.[0-9]+$` or scientific notation
- **Date**: Values match common date patterns (`YYYY-MM-DD`, `MM/DD/YYYY`,
  `DD-Mon-YYYY`, ISO 8601)
- **Boolean**: Values are exclusively from `{true, false, yes, no, 1, 0, t, f}`
- **Categorical**: String column with fewer than 20 unique values in the sample
- **Text**: String column with 20 or more unique values

Record the detected type for each column. If a column has mixed types, flag it
as "mixed" and note the percentage of each type.

## Step 3: Missing Value Analysis

Count missing values per column. Missing values include:
- Empty strings
- Literal strings: `NULL`, `null`, `NA`, `N/A`, `n/a`, `NaN`, `nan`, `-`, `#N/A`
- Whitespace-only strings

Use `awk` to count:

```
awk -F',' '
NR > 1 {
  for (i = 1; i <= NF; i++) {
    gsub(/^[ \t]+|[ \t]+$/, "", $i)
    if ($i == "" || $i == "NULL" || $i == "null" || $i == "NA" ||
        $i == "N/A" || $i == "n/a" || $i == "NaN" || $i == "nan" ||
        $i == "-" || $i == "#N/A")
      missing[i]++
    total[i]++
  }
}
END {
  for (i = 1; i <= NF; i++)
    printf "Column %d: %d missing of %d (%.1f%%)\n", i, missing[i]+0, total[i], (missing[i]+0)*100/total[i]
}
' <file_path>
```

Flag any column with more than 5% missing values as a data quality concern.
Flag any column with more than 50% missing values as a critical issue.

## Step 4: Numeric Statistics

For each numeric column (integer or float), compute:

```
awk -F',' -v col=<column_index> '
NR > 1 {
  v = $col + 0
  if ($col != "" && $col !~ /^(NULL|NA|NaN)$/) {
    sum += v; sumsq += v*v; n++
    vals[n] = v
    if (n == 1 || v < min) min = v
    if (n == 1 || v > max) max = v
  }
}
END {
  mean = sum / n
  variance = (sumsq / n) - (mean * mean)
  stddev = sqrt(variance)
  printf "count=%d min=%.4f max=%.4f mean=%.4f stddev=%.4f\n", n, min, max, mean, stddev
}
' <file_path>
```

For each numeric column, report:
- Count (non-null)
- Minimum
- Maximum
- Mean
- Standard deviation
- Range (max - min)

## Step 5: Outlier Detection

For each numeric column, identify values more than 3 standard deviations from
the mean. Use the mean and stddev computed in Step 4.

```
awk -F',' -v col=<column_index> -v mean=<mean> -v stddev=<stddev> '
NR > 1 {
  v = $col + 0
  if ($col != "" && $col !~ /^(NULL|NA|NaN)$/) {
    if (v > mean + 3*stddev || v < mean - 3*stddev)
      printf "Row %d: value=%s (%.1f std devs from mean)\n", NR, $col, (v - mean) / stddev
  }
}
' <file_path>
```

Report:
- Total outlier count per column
- The most extreme outlier per column with its row number
- If a column has more than 5% outliers, flag it as potentially having a
  non-normal distribution rather than true outliers

## Step 6: Duplicate Detection

Check for fully duplicate rows:

```
awk 'NR > 1' <file_path> | sort | uniq -d | wc -l
```

If duplicates exist, report:
- Total duplicate row count
- Show up to 5 sample duplicate rows

Also check for duplicates on likely key columns (first column, or any column
named `id`, `ID`, `key`, `uuid`):

```
awk -F',' 'NR > 1 { print $1 }' <file_path> | sort | uniq -d | head -10
```

## Step 7: Categorical Column Analysis

For each categorical column (fewer than 20 unique values), compute value
frequency distribution:

```
awk -F',' -v col=<column_index> '
NR > 1 {
  gsub(/^[ \t]+|[ \t]+$/, "", $col)
  count[$col]++
  total++
}
END {
  for (val in count)
    printf "%s: %d (%.1f%%)\n", val, count[val], count[val]*100/total
}
' <file_path> | sort -t: -k2 -rn
```

Flag any categorical column where a single value represents more than 95% of
rows (near-constant column, possibly useless for analysis).

## Step 8: Correlation Hints

For each pair of numeric columns (limit to first 10 numeric columns to avoid
combinatorial explosion), compute Pearson correlation using awk:

```
awk -F',' -v c1=<col_i> -v c2=<col_j> '
NR > 1 && $c1 != "" && $c2 != "" {
  x = $c1+0; y = $c2+0
  sx += x; sy += y; sxy += x*y; sx2 += x*x; sy2 += y*y; n++
}
END {
  num = n*sxy - sx*sy
  den = sqrt((n*sx2 - sx*sx) * (n*sy2 - sy*sy))
  if (den > 0) printf "r = %.4f\n", num/den
  else printf "r = undefined (zero variance)\n"
}
' <file_path>
```

Flag any pair with |r| > 0.8 as strongly correlated. Flag any pair with
|r| > 0.95 as potentially redundant (one column may be derivable from another).

## Step 9: Generate Report

Compile findings into this format:

```
# CSV Data Quality Report
**File:** <file_path>
**Generated:** <current date>

## File Overview
- Rows: <count> (excluding header)
- Columns: <count>
- File size: <size>
- Delimiter: <detected delimiter>
- Encoding issues: <yes/no>

## Column Summary
| # | Name | Type | Non-Null | Missing % | Unique Values |
|---|------|------|----------|-----------|---------------|
<rows>

## Numeric Statistics
| Column | Min | Max | Mean | Std Dev | Outliers |
|--------|-----|-----|------|---------|----------|
<rows>

## Data Quality Issues

### Critical
<>50% missing columns, fully duplicate rows>

### Warning
<>5% missing columns, outlier-heavy columns, mixed-type columns>

### Info
<Near-constant columns, strong correlations>

## Correlations
| Column A | Column B | Pearson r | Strength |
|----------|----------|-----------|----------|
<rows with |r| > 0.5, sorted by absolute value descending>

## Recommendations
1. <Specific actionable recommendation>
2. <Next recommendation>
...
```

If the user requested a file output, write the report using `file_write`.
Otherwise, return the report directly.
