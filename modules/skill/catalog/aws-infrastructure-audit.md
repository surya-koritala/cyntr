---
name: aws-infrastructure-audit
description: Comprehensive AWS account infrastructure audit across all enabled regions
version: 1.0.0
author: cyntr
tools:
  - name: shell_exec
  - name: file_write
---

# AWS Infrastructure Audit

You are an AWS infrastructure auditor. Your job is to produce a complete inventory of resources across all enabled regions in an AWS account and flag anything unusual.

## Step 1: Verify AWS Access

Run the following command to confirm you have valid credentials:

```
aws sts get-caller-identity --output json
```

If this fails, stop immediately and tell the user their AWS credentials are not configured. Extract the Account ID and ARN from the output — you will include these in the final report header.

## Step 2: Discover Enabled Regions

Run:

```
aws ec2 describe-regions --query "Regions[?OptInStatus!='not-opted-in'].RegionName" --output json
```

Store the resulting list. These are the regions you will iterate through. If the list is empty or the command fails, fall back to these common regions: us-east-1, us-west-2, eu-west-1, ap-southeast-1.

## Step 3: Global Resources (run once, not per-region)

### S3 Buckets

Run:

```
aws s3api list-buckets --query "Buckets[].Name" --output json
```

For each bucket, check its public access configuration:

```
aws s3api get-public-access-block --bucket BUCKET_NAME --output json 2>/dev/null || echo "NO_PUBLIC_ACCESS_BLOCK"
```

If `get-public-access-block` returns an error or any of the four block settings is `false`, flag the bucket as **potentially public**. Also check the bucket ACL:

```
aws s3api get-bucket-acl --bucket BUCKET_NAME --query "Grants[?Grantee.URI=='http://acs.amazonaws.com/groups/global/AllUsers']" --output json
```

If this returns any grants, flag the bucket as **PUBLIC via ACL — CRITICAL**.

### CloudFront Distributions

Run:

```
aws cloudfront list-distributions --query "DistributionList.Items[].{Id:Id,Domain:DomainName,Status:Status,Origins:Origins.Items[0].DomainName}" --output json
```

Record the distribution count and list each with its ID, domain, and status.

## Step 4: Per-Region Resource Inventory

For EACH region from Step 2, run the following commands. Use the `--region REGION` flag on every command.

### EC2 Instances

```
aws ec2 describe-instances --region REGION --query "Reservations[].Instances[].{Id:InstanceId,Type:InstanceType,State:State.Name,LaunchTime:LaunchTime,PublicIp:PublicIpAddress,Name:Tags[?Key=='Name']|[0].Value}" --output json
```

Flag any instance where `State` is `stopped` — these still incur EBS charges. Flag any instance with a `PublicIp` that is not behind a load balancer.

### RDS Instances

```
aws rds describe-db-instances --region REGION --query "DBInstances[].{Id:DBInstanceIdentifier,Engine:Engine,Class:DBInstanceClass,Status:DBInstanceStatus,MultiAZ:MultiAZ,Public:PubliclyAccessible,Encrypted:StorageEncrypted}" --output json
```

Flag any instance where `PubliclyAccessible` is `true`. Flag any instance where `StorageEncrypted` is `false`. Flag any instance where `MultiAZ` is `false` for production-looking names.

### ECS Clusters and Services

```
aws ecs list-clusters --region REGION --query "clusterArns" --output json
```

For each cluster ARN:

```
aws ecs describe-clusters --region REGION --clusters CLUSTER_ARN --query "clusters[].{Name:clusterName,Status:status,RunningTasks:runningTasksCount,Services:activeServicesCount}" --output json
```

### Lambda Functions

```
aws lambda list-functions --region REGION --query "Functions[].{Name:FunctionName,Runtime:Runtime,Memory:MemorySize,Timeout:Timeout,LastModified:LastModified}" --output json
```

Flag any function using a deprecated runtime (python2.7, nodejs10.x, nodejs12.x, dotnetcore2.1, ruby2.5).

### Load Balancers (ELB/ALB/NLB)

```
aws elbv2 describe-load-balancers --region REGION --query "LoadBalancers[].{Name:LoadBalancerName,Type:Type,Scheme:Scheme,State:State.Code,DNSName:DNSName}" --output json
```

Also check classic load balancers:

```
aws elb describe-load-balancers --region REGION --query "LoadBalancerDescriptions[].{Name:LoadBalancerName,Scheme:Scheme,DNSName:DNSName}" --output json
```

## Step 5: Compile the Report

Use `file_write` to create a file called `aws-audit-report.md` in the current working directory. Structure the report as follows:

```markdown
# AWS Infrastructure Audit Report

**Account:** <account-id>
**Identity:** <arn>
**Date:** <current date/time>
**Regions Scanned:** <count>

## Summary

| Resource Type | Total Count |
|---|---|
| EC2 Instances | X |
| RDS Instances | X |
| ECS Clusters | X |
| Lambda Functions | X |
| Load Balancers | X |
| S3 Buckets | X |
| CloudFront Distributions | X |

## Findings (Action Required)

### CRITICAL
- [list any public S3 buckets, public RDS instances]

### WARNING
- [list stopped EC2 instances, deprecated Lambda runtimes, unencrypted RDS]

### INFO
- [list other notable items]

## Resource Details by Region

### us-east-1
#### EC2 Instances
[table of instances]
...
```

## Step 6: Present Results

After writing the file, print a summary to the user with:
- Total resource count
- Number of findings by severity
- The path to the full report file
- The top 3 most urgent findings that need attention

If any region returns an access denied error, note it in the report but continue with other regions. Do not stop the entire audit for a single region failure.
