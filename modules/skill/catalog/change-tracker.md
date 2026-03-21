---
name: change-tracker
description: Track and audit infrastructure changes across git, cloud APIs, and CI/CD pipelines
version: 1.0.0
author: cyntr
tools:
  - name: shell_exec
  - name: http_request
  - name: file_read
  - name: file_write
---

# Infrastructure Change Tracker

Monitor and audit changes to infrastructure-as-code, cloud resources, and
deployment pipelines. Detect unauthorized changes, missing approvals, and
generate a timestamped change log for compliance reporting.

## Prerequisites

- Git repository access (for IaC change history).
- GitHub CLI (`gh`) for PR and approval data (optional but recommended).
- AWS CLI for CloudTrail data (optional).
- The user should specify the time window to audit (default: last 7 days).

## Step 1: Set Audit Window

Determine the time range:

```
AUDIT_START=$(date -v-7d +%Y-%m-%dT00:00:00Z 2>/dev/null || date -d "7 days ago" +%Y-%m-%dT00:00:00Z)
AUDIT_END=$(date +%Y-%m-%dT23:59:59Z)
```

If the user specifies a custom window, use their dates. Store these for all
subsequent queries.

## Step 2: Track Infrastructure-as-Code Changes

Identify IaC files in the repository. Common patterns:

```
# Terraform
find . -name "*.tf" -not -path "*/.terraform/*" 2>/dev/null | head -50

# CloudFormation
find . -name "*.yaml" -path "*/cloudformation/*" -o -name "*.yaml" -path "*/cfn/*" 2>/dev/null | head -50
find . -name "*.template" 2>/dev/null | head -20

# Ansible
find . -name "*.yml" -path "*/ansible/*" -o -name "*.yml" -path "*/playbooks/*" 2>/dev/null | head -50

# Kubernetes manifests
find . -name "*.yaml" -path "*/k8s/*" -o -name "*.yaml" -path "*/kubernetes/*" -o -name "*.yaml" -path "*/manifests/*" 2>/dev/null | head -50

# Helm charts
find . -name "Chart.yaml" 2>/dev/null | head -20

# Dockerfiles
find . -name "Dockerfile*" 2>/dev/null | head -20

# CI/CD configs
ls .github/workflows/*.yml .gitlab-ci.yml Jenkinsfile .circleci/config.yml 2>/dev/null
```

For each category of IaC file found, query git for changes in the audit window:

```
git log --since="$AUDIT_START" --until="$AUDIT_END" --oneline --name-only -- "*.tf" "*.yaml" "Dockerfile*" ".github/workflows/*"
```

For each commit that modified IaC files, record:
- Commit hash
- Author name and email
- Timestamp
- Commit message
- List of files changed
- Lines added and removed: `git show --stat <hash> -- <iac_files>`

## Step 3: Check PR Approval Status

For each IaC-changing commit, determine whether it went through proper review:

```
# Find the PR that contains this commit
gh pr list --state merged --search "<commit_hash>" --json number,title,author,mergedAt,reviews,mergedBy 2>/dev/null
```

If `gh` is not available, check merge commit messages for PR references:

```
git log --since="$AUDIT_START" --merges --oneline | grep -oP '#\d+'
```

For each PR found, check:
- **Was it reviewed?** At least one approving review required.
- **Who approved it?** The approver should not be the same as the author.
- **Was it merged by someone other than the author?** (Four-eyes principle.)
- **Did CI pass before merge?**

```
gh pr view <pr_number> --json reviews,statusCheckRollup,author,mergedBy 2>/dev/null
```

Classify each change:
- **Approved**: PR with at least one approval from a different person, CI passed.
- **Self-approved**: Author merged their own PR without external review.
- **No PR**: Commit was pushed directly to main without a PR.
- **CI bypassed**: PR merged with failing or skipped CI checks.

## Step 4: Check Direct Pushes to Protected Branches

Detect commits pushed directly to main/master without a PR:

```
# Get all commits on main in the audit window
git log main --since="$AUDIT_START" --until="$AUDIT_END" --oneline --no-merges > /tmp/main_commits.txt

# Get all merge commits (these are PR merges)
git log main --since="$AUDIT_START" --until="$AUDIT_END" --oneline --merges > /tmp/merge_commits.txt

# Direct pushes are non-merge commits on main
# (This is an approximation; some merge strategies create non-merge commits)
```

For each direct push, record it as a potential policy violation. Direct pushes
to protected branches bypass review and are flagged as **unauthorized** unless
the branch protection allows it.

## Step 5: Query AWS CloudTrail (if available)

Check for cloud infrastructure changes via CloudTrail:

```
# Look for infrastructure-modifying API calls
aws cloudtrail lookup-events \
  --start-time "$AUDIT_START" \
  --end-time "$AUDIT_END" \
  --lookup-attributes AttributeKey=EventName,AttributeValue=RunInstances \
  --query 'Events[*].[EventTime,Username,EventName,Resources[0].ResourceName]' \
  --output table 2>/dev/null
```

Key event names to check (each is a separate query):

| Category | Event Names |
|----------|------------|
| EC2 | RunInstances, TerminateInstances, ModifyInstanceAttribute |
| Security Groups | AuthorizeSecurityGroupIngress, RevokeSecurityGroupIngress, CreateSecurityGroup, DeleteSecurityGroup |
| IAM | CreateUser, DeleteUser, AttachUserPolicy, CreateRole, PutRolePolicy |
| S3 | CreateBucket, DeleteBucket, PutBucketPolicy, PutBucketAcl |
| RDS | CreateDBInstance, DeleteDBInstance, ModifyDBInstance |
| VPC | CreateVpc, DeleteVpc, CreateSubnet, ModifySubnetAttribute |
| Lambda | CreateFunction, UpdateFunctionCode, UpdateFunctionConfiguration |
| KMS | CreateKey, DisableKey, ScheduleKeyDeletion |

For each event found, record:
- Event time
- User/role that made the change
- Event name (what was changed)
- Resource affected
- Source IP address (if available)

Flag any changes made:
- Outside of business hours (before 8 AM or after 8 PM local time)
- By the root account
- By an unknown or unexpected IAM user/role
- That modify security controls (security groups, IAM, KMS)

## Step 6: Check Deployment Pipeline Activity

Query CI/CD for deployment activity:

```
# GitHub Actions
gh run list --workflow=deploy --created=">=$AUDIT_START" --json databaseId,status,conclusion,createdAt,headBranch,actor --limit 50 2>/dev/null

# Or check all workflow runs
gh run list --created=">=$AUDIT_START" --json databaseId,workflowName,status,conclusion,createdAt,headBranch,actor --limit 50 2>/dev/null
```

For each deployment:
- Was it triggered by a merged PR or manually?
- Did it succeed or fail?
- Was there a rollback afterward?
- Who triggered it?

Detect rollbacks by looking for:
- Reverts: `git log --since="$AUDIT_START" --grep="revert" --oneline`
- Re-deployments of older commits
- Manual workflow dispatches shortly after a failed deploy

## Step 7: Correlate Changes

Build a timeline correlating all change sources:

1. Sort all events (git commits, PR merges, CloudTrail events, deployments)
   by timestamp.
2. Group events that are likely related (e.g., a PR merge followed by a
   deployment within 30 minutes).
3. Identify orphaned changes: cloud changes with no corresponding IaC commit.
4. Identify "drift": IaC changes that were never deployed, or cloud changes
   that are not reflected in IaC.

## Step 8: Generate Change Log Report

```
# Infrastructure Change Log
**Audit Period:** <start_date> to <end_date>
**Generated:** <current datetime>

## Executive Summary
- **Total changes tracked:** <count>
- **Approved changes:** <count> (<pct>%)
- **Unauthorized changes:** <count>
- **Direct pushes:** <count>
- **Cloud-only changes (no IaC):** <count>

## Change Timeline

| # | Timestamp | Type | Author | Description | Approval | Status |
|---|-----------|------|--------|-------------|----------|--------|
| 1 | <datetime> | IaC/Cloud/Deploy | <name> | <summary> | Approved/Self/None | OK/FLAG |

## Flagged Changes

### Unauthorized (No PR Approval)
| Commit/Event | Author | Timestamp | Details |
|-------------|--------|-----------|---------|
<rows>

### Self-Approved
| PR | Author | Timestamp | Details |
|----|--------|-----------|---------|
<rows>

### Direct Pushes to Main
| Commit | Author | Timestamp | Files Changed |
|--------|--------|-----------|---------------|
<rows>

### Cloud Changes Without IaC
| Event | User | Timestamp | Resource |
|-------|------|-----------|----------|
<rows>

### Off-Hours Changes
| Event | User | Timestamp | Details |
|-------|------|-----------|---------|
<rows>

## IaC Files Changed
| File | Commits | Authors | Net Lines Changed |
|------|---------|---------|-------------------|
<rows>

## Deployment Log
| # | Timestamp | Branch | Triggered By | Status | Duration |
|---|-----------|--------|-------------|--------|----------|
<rows>

## Recommendations
1. <Specific remediation for flagged items>
2. <Process improvement suggestions>

## Compliance Notes
- All changes should be reviewed and approved before deployment.
- Direct pushes to protected branches should be disabled via branch protection.
- Cloud changes should be made exclusively through IaC to maintain auditability.

---
*Generated by cyntr/change-tracker v1.0.0*
```

## Step 9: Output

Write the report to file (suggested: `change-log-<start>-to-<end>.md`).
Return the full report as the response.

If any unauthorized changes are detected, prominently warn the user at the
beginning of the response.
