---
name: status-dashboard
description: Generate a traffic-light status dashboard for services, infrastructure, and deployments
version: 1.0.0
author: cyntr
tools:
  - name: http_request
  - name: shell_exec
  - name: file_read
  - name: file_write
  - name: knowledge_search
---

# System Status Dashboard

Aggregate health signals from application endpoints, infrastructure, recent
deployments, error logs, and incident records to produce a traffic-light status
overview across all monitored services.

## Prerequisites

- The user should provide a list of services to monitor or a configuration file
  describing the services and their health check endpoints.
- Shell access is needed for infrastructure commands.
- HTTP access is needed for health endpoints.

## Step 1: Load Service Configuration

If the user provides a configuration file, read it with `file_read`. Expected
format:

```json
{
  "services": [
    {
      "name": "User API",
      "category": "application",
      "health_url": "https://api.example.com/health",
      "expected_status": 200,
      "log_path": "/var/log/user-api/error.log",
      "repo": "org/user-api"
    }
  ]
}
```

If no config is provided, ask the user to list services or attempt discovery:

```
# Discover running services via Docker
docker ps --format "{{.Names}}\t{{.Status}}\t{{.Ports}}" 2>/dev/null

# Discover Kubernetes services
kubectl get services -o wide 2>/dev/null

# Discover systemd services
systemctl list-units --type=service --state=running 2>/dev/null | head -30
```

## Step 2: Check Application Health Endpoints

For each service with a `health_url`, make an HTTP request:

```
curl -s -o /tmp/health_response_<service> -w "%{http_code}|%{time_total}" \
  --max-time 10 "<health_url>"
```

Parse the response and classify:

- **Green**: HTTP 200, response time < 1 second, response body indicates
  healthy (look for `"status":"ok"`, `"healthy":true`, `"status":"UP"`).
- **Yellow**: HTTP 200 but slow (> 1 second), or response body indicates
  degraded (e.g., `"status":"degraded"`, some sub-checks failing).
- **Red**: Non-200 status code, timeout, connection refused, or response body
  indicates unhealthy.

For health endpoints that return detailed sub-checks (common in Spring Boot
Actuator, ASP.NET health checks), parse each sub-check and report individually.

## Step 3: Check Infrastructure Status

Run infrastructure health commands based on what is available:

### AWS (if AWS CLI configured)
```
# EC2 instance status
aws ec2 describe-instance-status --query 'InstanceStatuses[*].[InstanceId,InstanceState.Name,SystemStatus.Status,InstanceStatus.Status]' --output table 2>/dev/null

# RDS status
aws rds describe-db-instances --query 'DBInstances[*].[DBInstanceIdentifier,DBInstanceStatus]' --output table 2>/dev/null

# ECS/EKS service health
aws ecs describe-services --cluster <cluster> --services <service_list> --query 'services[*].[serviceName,status,runningCount,desiredCount]' --output table 2>/dev/null
```

### Kubernetes (if kubectl configured)
```
# Node status
kubectl get nodes -o wide 2>/dev/null

# Pod health
kubectl get pods --all-namespaces -o wide 2>/dev/null | grep -v "Running\|Completed"

# Recent events (errors)
kubectl get events --sort-by='.lastTimestamp' --field-selector type=Warning 2>/dev/null | tail -10
```

### Docker (if Docker available)
```
docker ps --format "table {{.Names}}\t{{.Status}}\t{{.Ports}}" 2>/dev/null
docker ps --filter "status=exited" --format "{{.Names}}: exited {{.Status}}" 2>/dev/null
```

### System resources
```
# Disk usage (flag >80% as yellow, >90% as red)
df -h | awk 'NR>1 { gsub(/%/,"",$5); if ($5+0 > 80) print $0 }'

# Memory usage
free -m 2>/dev/null || vm_stat 2>/dev/null

# Load average (flag >CPU_count as yellow, >2*CPU_count as red)
uptime
nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null
```

Classify each infrastructure component as green, yellow, or red based on the
criteria noted above.

## Step 4: Check Recent Deployments

Look for recent deployments via git and GitHub:

```
# Recent tags (likely releases)
git tag --sort=-creatordate | head -5

# Recent merges to main
git log main --merges --oneline --since="48 hours ago" 2>/dev/null
```

If `gh` CLI is available:
```
# Recent releases
gh release list --limit 5 2>/dev/null

# Recent deployments (via GitHub Actions)
gh run list --workflow=deploy --limit 5 --json status,conclusion,createdAt,headBranch 2>/dev/null
```

Classify deployment status:
- **Green**: Last deployment succeeded, no rollbacks.
- **Yellow**: Deployment in progress or deployed within last 2 hours (monitoring
  window).
- **Red**: Last deployment failed or was rolled back.

## Step 5: Check Error Rates from Logs

For each service with a `log_path`, analyze recent error rates:

```
# Count errors in last hour
ERROR_COUNT=$(grep -c "ERROR\|FATAL\|CRITICAL" <log_path> 2>/dev/null | tail -1)

# Count total log lines in last hour (approximate)
TOTAL_COUNT=$(wc -l < <log_path> 2>/dev/null)

# Recent errors (last 10)
grep "ERROR\|FATAL\|CRITICAL" <log_path> 2>/dev/null | tail -10
```

If log files include timestamps, filter to the last hour:
```
HOUR_AGO=$(date -v-1H +"%Y-%m-%d %H" 2>/dev/null || date -d "1 hour ago" +"%Y-%m-%d %H")
grep "$HOUR_AGO" <log_path> | grep -c "ERROR"
```

Classify:
- **Green**: Error rate < 1% of total log volume, no FATAL/CRITICAL.
- **Yellow**: Error rate 1-5%, or any CRITICAL errors.
- **Red**: Error rate > 5%, or any FATAL errors, or errors increasing over time.

## Step 6: Check Open Incidents

Search the knowledge base for open incidents:

```
knowledge_search("open incident" OR "ongoing issue" OR "outage")
```

Also check for any incident management signals:
- PagerDuty-style status pages (if URL provided)
- Recent GitHub issues labeled "bug" or "incident"

```
gh issue list --label "bug,incident,outage" --state open --json number,title,createdAt 2>/dev/null
```

## Step 7: Generate Dashboard

Compile all findings into the traffic-light dashboard:

```
# System Status Dashboard
**Generated:** <current datetime>
**Overall Status:** <GREEN | YELLOW | RED>

## Service Status Overview

| Service | Health | Infra | Deploy | Errors | Incidents | Overall |
|---------|--------|-------|--------|--------|-----------|---------|
| <name>  | GREEN  | GREEN | GREEN  | GREEN  | GREEN     | GREEN   |
| <name>  | YELLOW | GREEN | GREEN  | YELLOW | GREEN     | YELLOW  |
| <name>  | RED    | GREEN | RED    | RED    | GREEN     | RED     |

## Status Legend
- **GREEN**: Fully operational, no issues detected.
- **YELLOW**: Degraded performance or warning conditions present.
- **RED**: Service down, critical errors, or active incidents.

## Details

### <Service Name>: <STATUS>
- **Health endpoint**: <status code>, <latency>ms
- **Infrastructure**: <details>
- **Last deployment**: <date>, <status>
- **Error rate**: <count> errors in last hour (<pct>%)
- **Open incidents**: <count>

<repeat for each service>

## Infrastructure Summary
| Component | Status | Details |
|-----------|--------|---------|
| Disk      | <color> | <usage details> |
| Memory    | <color> | <usage details> |
| CPU Load  | <color> | <load average>   |
| Nodes     | <color> | <count healthy/total> |

## Recent Deployments
| Service | When | Branch | Status |
|---------|------|--------|--------|
<rows>

## Active Alerts
<List of any red or yellow items that need attention, sorted by severity>

## Recommended Actions
1. <Most urgent action>
2. <Next action>
...

---
*Dashboard generated by cyntr/status-dashboard v1.0.0*
```

Determine overall status:
- **GREEN**: All services green.
- **YELLOW**: Any service yellow, none red.
- **RED**: Any service red.

## Step 8: Output

Write the dashboard to file if requested (suggested: `status-<datetime>.md`).
Otherwise, return as the response.

For continuous monitoring, the user can re-run this skill at intervals. Each
run produces a point-in-time snapshot.
