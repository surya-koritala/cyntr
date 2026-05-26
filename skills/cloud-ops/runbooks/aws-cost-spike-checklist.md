# AWS Cost Spike — 8-Step Investigation Checklist

Use this when a cost anomaly fires or someone asks "why did the bill go up?"
Time-box: 20 minutes for steps 1-6, then escalate if no clear cause.

## 1. Confirm the spike is real

Pull Cost Explorer GroupBy=SERVICE for the suspect window and the same window
one period prior (week-over-week is the default; month-over-month for slow
drifts). Anomaly < 10% of baseline is usually noise — close it out.

## 2. Identify the top three drivers

Sort the delta (current minus baseline) descending. Anything contributing less
than 5% of the total delta is a distraction at this stage. Note the dollar
amount, the service, and the linked account for each.

## 3. Narrow the time window

Re-run the top driver with daily granularity. Is the spike a single day, a step
change, or a slope? Single-day spike = backfill or migration. Step change =
new resource provisioned. Slope = organic growth or a leak.

## 4. Drill into the specific service

- **EC2:** `describe-instances --filters Name=launch-time,Values=>=<date>` —
  look for new instance types, especially the GPU families and metal sizes.
- **S3:** CloudWatch metric `BucketSizeBytes` and `NumberOfObjects` per bucket.
  Lifecycle policy gaps are the usual culprit.
- **RDS:** describe new instances and read replicas. Check `db.r6g.16xlarge`
  appearing where it didn't exist.
- **Data transfer:** Check NAT Gateway processing bytes per AZ. Cross-AZ chatter
  from a misconfigured service is a classic.
- **CloudWatch:** Log groups with no retention policy. One Lambda misconfigured
  can balloon log spend in a week.

## 5. Cross-reference recent changes

Was there a deploy, a Terraform apply, or an org-wide policy change in the
same window? Correlation is not causation but it narrows the search by 90%.

## 6. Quantify the recommendation

Don't say "consider rightsizing." Say: "this RDS instance is at 4% CPU; moving
from r6g.4xlarge to r6g.large saves ~$1,800/month." Numbers force decisions.

## 7. Decide blast radius before remediating

Even read-only remediation suggestions (lifecycle policy, retention, instance
type) need a human-in-the-loop approval. Cyntr's policy engine should route
any modify intent through `require_approval`.

## 8. Document the root cause

Drop the finding into the team's runbook channel. Three lines: what changed,
what it cost, what stopped it. Next time it happens, search beats investigate.
