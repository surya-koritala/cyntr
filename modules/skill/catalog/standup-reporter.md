---
name: standup-reporter
description: Generate daily standup reports from git, GitHub PRs, and issue trackers
version: 1.0.0
author: cyntr
tools:
  - name: shell_exec
  - name: http_request
  - name: file_read
  - name: file_write
---

# Daily Standup Reporter

Automatically generate standup reports by aggregating activity from git history,
GitHub pull requests, and issue trackers. Produces per-person or team summaries
in the standard "Done / Doing / Blockers" format.

## Prerequisites

- Must be run inside a git repository for commit history.
- GitHub CLI (`gh`) should be available for PR data (optional).
- Jira API access via `http_request` for ticket data (optional).
- The user may specify a team roster or let the skill discover contributors
  from git history.

## Step 1: Determine Time Window and Team

Set the standup window to the last 24 hours (or since last business day if
today is Monday):

```
DAY_OF_WEEK=$(date +%u)
if [ "$DAY_OF_WEEK" -eq 1 ]; then
  SINCE="last friday"
else
  SINCE="24 hours ago"
fi
```

If the user provides a team roster (list of names or git author emails), use it.
Otherwise, discover contributors from recent history:

```
git log --since="$SINCE" --format="%aN <%aE>" | sort -u
```

Store the contributor list for per-person report generation.

## Step 2: Collect Git Activity

For each contributor, retrieve their commits:

```
git log --since="$SINCE" --author="<contributor_email>" --oneline --no-merges
```

Group commits by repository (if in a monorepo, group by top-level directory).

Also check for merge commits that indicate PR merges:

```
git log --since="$SINCE" --author="<contributor_email>" --merges --oneline
```

For each commit, extract:
- Short hash
- Commit message (first line)
- Files changed: `git diff-tree --no-commit-id --name-only -r <hash>`

Categorize commits by type based on message prefixes or conventional commit
format:
- `feat:` or `feature` = New feature
- `fix:` or `bugfix` = Bug fix
- `refactor:` = Refactoring
- `docs:` = Documentation
- `test:` = Testing
- `chore:` = Maintenance
- Other = General work

## Step 3: Collect GitHub PR Activity (if available)

Check if `gh` CLI is available:

```
which gh 2>/dev/null && gh auth status 2>/dev/null
```

If available, pull PR data:

```
gh pr list --state all --search "author:<username> updated:>=$(date -v-1d +%Y-%m-%d 2>/dev/null || date -d '1 day ago' +%Y-%m-%d)" --json number,title,state,url,reviewDecision,additions,deletions
```

For each contributor, categorize PRs:
- **Merged**: PRs that were merged in the window
- **Opened**: PRs that were created in the window
- **Reviewed**: PRs where the contributor left a review

Also check for PRs awaiting review from the contributor:

```
gh pr list --search "review-requested:<username>" --json number,title,url
```

## Step 4: Collect Issue Tracker Activity (if available)

If Jira API credentials are available, query for ticket updates:

```
curl -s -H "Authorization: Basic <base64_credentials>" \
  "https://<domain>.atlassian.net/rest/api/3/search?jql=assignee=<user>+AND+updated>='-1d'&fields=key,summary,status,priority"
```

For each ticket, extract:
- Ticket key and summary
- Current status
- Status transitions in the window (moved to In Progress, Done, etc.)

If no Jira access, check for local task files or TODO comments:

```
git log --since="$SINCE" --all -p | grep -i "TODO\|FIXME\|HACK\|BLOCKED" | head -20
```

## Step 5: Detect Blockers

Identify potential blockers from multiple signals:

1. **PR blockers**: PRs with requested changes or failing CI:
   ```
   gh pr list --author=<username> --json number,title,reviewDecision,statusCheckRollup | jq '.[] | select(.reviewDecision == "CHANGES_REQUESTED" or (.statusCheckRollup[]?.conclusion == "FAILURE"))'
   ```

2. **Stale PRs**: PRs open for more than 3 days without review:
   ```
   gh pr list --author=<username> --json number,title,createdAt | jq '.[] | select((.createdAt | fromdateiso8601) < (now - 259200))'
   ```

3. **Ticket blockers**: Jira tickets with "Blocked" status or blocker priority.

4. **Explicit blockers**: Commit messages or PR descriptions containing
   "blocked", "waiting on", "depends on", "need help".

## Step 6: Infer "Doing Today"

Estimate what each contributor is likely working on today:

1. **Open PRs**: PRs that are still in review or draft = likely active work.
2. **In-progress tickets**: Jira tickets in "In Progress" status.
3. **Recent branches**: Branches with very recent commits that are not yet merged:
   ```
   git branch --sort=-committerdate --format='%(refname:short) %(committerdate:relative)' | head -5
   ```
4. **Carry-over items**: Work started yesterday that was not completed.

## Step 7: Generate Per-Person Standup

For each contributor, format their standup:

```
### <Contributor Name>

**What I did:**
- Merged PR #<num>: <title> (+<additions>/-<deletions>)
- Committed <count> changes to <area>: <brief summary>
- Completed <ticket>: <summary>
- Reviewed PR #<num>: <title>

**What I'm doing today:**
- Continuing work on PR #<num>: <title> (in review)
- Working on <ticket>: <summary> (in progress)
- <branch_name>: <inferred from recent commits>

**Blockers:**
- PR #<num> has failing CI checks since <date>
- Waiting on review for PR #<num> (open <n> days)
- <ticket> blocked by <dependency>
```

If no blockers were detected, write: "No blockers identified."

## Step 8: Generate Team Summary

Aggregate individual standups into a team report:

```
# Daily Standup Report
**Date:** <current date>
**Team:** <team name or "All Contributors">
**Period:** Since <start datetime>

## Team Summary
- **Commits:** <total count>
- **PRs merged:** <count>
- **PRs opened:** <count>
- **Tickets completed:** <count>
- **Active blockers:** <count>

## Individual Reports

<Per-person standups from Step 7>

## Team Blockers
<Aggregated list of all blockers, sorted by severity>

## Activity Heatmap
| Contributor | Commits | PRs | Reviews | Tickets |
|-------------|---------|-----|---------|---------|
<rows>

---
*Generated by cyntr/standup-reporter v1.0.0*
```

## Step 9: Output

If the user specified an output file, write with `file_write`. Default
filename suggestion: `standup-<date>.md`.

If the user wants the standup sent to a channel or tool, format it as plain
text suitable for Slack or Teams (replace markdown tables with simple lists).

Return the complete report as the response.
