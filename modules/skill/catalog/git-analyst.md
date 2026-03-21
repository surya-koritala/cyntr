---
name: git-analyst
description: Analyze git repository health, contributor patterns, and codebase hotspots
version: 1.0.0
author: cyntr
tools:
  - name: shell_exec
  - name: file_read
  - name: file_write
---

# Git Repository Analyst

Perform comprehensive analysis of a git repository to surface commit patterns,
contributor activity, codebase hotspots, and potential maintenance risks.

## Prerequisites

- The current working directory must be inside a valid git repository.
- Run `git rev-parse --is-inside-work-tree` to confirm. If it returns `false`
  or errors, stop and inform the user that no git repository was found.

## Step 1: Gather Recent History

Run the following commands and capture their output:

```
git log --oneline -50
git log --oneline --since="30 days ago" | wc -l
git log --oneline --since="7 days ago" | wc -l
```

Record the total commits in the last 50, the count from the last 30 days, and
the count from the last 7 days. Compute average commits per day for each window.

## Step 2: Contributor Statistics

Run:

```
git shortlog -sn --no-merges --since="90 days ago"
```

For each contributor, record their commit count. Compute each contributor's
percentage of total commits. Flag any contributor responsible for more than 60%
of commits as a "bus factor risk" (over-reliance on a single person).

Also run:

```
git shortlog -sn --no-merges --since="7 days ago"
```

Compare the 7-day active contributors against the 90-day list. Note any
contributors who have gone inactive (present in 90-day but absent in 7-day).

## Step 3: Codebase Hotspots

Identify the most frequently changed files in the last 90 days:

```
git log --since="90 days ago" --pretty=format: --name-only | sort | uniq -c | sort -rn | head -20
```

These are "hotspot" files. Files that change very frequently are candidates for:
- Refactoring (they may be doing too much)
- Better test coverage (frequent changes increase regression risk)
- Code review attention (high-churn files accumulate complexity)

For each of the top 5 hotspot files, run:

```
git log --oneline --since="90 days ago" -- <file>
```

Summarize how many commits touched each file and the nature of those changes.

## Step 4: Large Commits

Find commits with an unusually high number of changed files:

```
git log --since="90 days ago" --pretty=format:"%h %s" --shortstat | paste - - -
```

Flag any commit that changes more than 20 files or more than 500 lines as a
"large commit." Large commits are harder to review and more likely to introduce
bugs. List each flagged commit with its hash, message, and stats.

## Step 5: Test Discipline Check

Search for commits that modify source files but do not modify any test files:

```
git log --since="30 days ago" --name-only --pretty=format:"COMMIT:%h %s" | awk '
  /^COMMIT:/ { commit=$0; has_src=0; has_test=0; next }
  /test|spec|_test\./ { has_test=1; next }
  /\.(js|ts|py|go|rs|java|rb|swift|kt)$/ { has_src=1; next }
  /^$/ { if (has_src && !has_test) print commit }
'
```

Report the percentage of source-changing commits that also include test changes.
If below 50%, flag this as a testing discipline concern.

## Step 6: Force Push Detection

Check the reflog for force pushes (only works on the local clone):

```
git reflog --since="30 days ago" | grep -i "forced"
```

If any force pushes are found, list them with timestamps. Force pushes on shared
branches can cause data loss and should be flagged as high severity.

## Step 7: Branch Hygiene

List stale branches (no commits in the last 30 days):

```
git for-each-ref --sort=-committerdate --format='%(refname:short) %(committerdate:relative)' refs/heads/ | while read branch date; do
  last=$(git log -1 --format=%ct "$branch" 2>/dev/null)
  threshold=$(date -v-30d +%s 2>/dev/null || date -d "30 days ago" +%s 2>/dev/null)
  if [ -n "$last" ] && [ "$last" -lt "$threshold" ]; then
    echo "$branch ($date)"
  fi
done
```

Report the total number of local branches and how many are stale.

## Step 8: Generate Report

Compile all findings into a structured markdown report with these sections:

```
# Git Repository Health Report
**Generated:** <current date>
**Repository:** <repo name from basename of git root>

## Summary
- Total commits (last 90 days): <count>
- Active contributors (last 7 days): <count>
- Hotspot files: <count of files with >10 changes>
- Health score: <Good | Needs Attention | At Risk>

## Commit Activity
<Daily/weekly averages, trend direction>

## Contributors
| Name | Commits (90d) | % of Total | Last Active |
|------|---------------|------------|-------------|
<rows>

## Codebase Hotspots
| File | Changes (90d) | Risk |
|------|--------------|------|
<rows>

## Flagged Issues
### Large Commits
<list or "None found">

### Missing Tests
<percentage and flagged commits>

### Force Pushes
<list or "None detected">

### Stale Branches
<list or "All branches active">

## Recommendations
<Actionable items based on findings>
```

Assign the overall health score:
- **Good**: No force pushes, test discipline >50%, no single contributor >60%
- **Needs Attention**: One or two of the above conditions violated
- **At Risk**: Three or more conditions violated

If the user requested a file output, write the report using `file_write`.
Otherwise, return the report directly as the response.
