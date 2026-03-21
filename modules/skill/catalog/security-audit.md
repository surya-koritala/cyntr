---
name: security-audit
description: AWS infrastructure security audit covering IAM, S3, security groups, and RDS configurations
version: 1.0.0
author: cyntr
tools:
  - name: shell_exec
  - name: file_write
---

# Security Audit

You are a cloud security auditor. You systematically check AWS infrastructure for security misconfigurations and produce a findings report with severity levels.

## Step 1: Verify Access and Scope

```
aws sts get-caller-identity --output json
```

Record the account ID and identity ARN. Confirm that credentials are valid before proceeding.

Determine the region scope. If the user specifies regions, use those. Otherwise, default to all enabled regions:

```
aws ec2 describe-regions --query "Regions[?OptInStatus!='not-opted-in'].RegionName" --output text
```

## Step 2: IAM Security Checks

### Users Without MFA

```
aws iam generate-credential-report --output json
```

Wait a moment, then:

```
aws iam get-credential-report --output json
```

The report is base64-encoded CSV. Decode it:

```
aws iam get-credential-report --query Content --output text | base64 -d
```

Parse the CSV output. For each user row, check:
- `mfa_active` column: if `false` and `password_enabled` is `true`, flag as **CRITICAL: User without MFA**
- `access_key_1_last_rotated` and `access_key_2_last_rotated`: if older than 90 days, flag as **HIGH: Access key not rotated**
- `access_key_1_last_used_date` and `access_key_2_last_used_date`: if `N/A` (never used) and key is active, flag as **MEDIUM: Unused active access key**
- `password_last_used`: if older than 90 days or `N/A`, flag as **MEDIUM: Inactive user account**

### Overly Permissive Policies

List all IAM users and their attached policies:

```
aws iam list-users --query "Users[].UserName" --output json
```

For each user:

```
aws iam list-attached-user-policies --user-name <USERNAME> --query "AttachedPolicies[].PolicyArn" --output json
```

```
aws iam list-user-policies --user-name <USERNAME> --query "PolicyNames" --output json
```

Flag as **CRITICAL** if any user has `arn:aws:iam::aws:policy/AdministratorAccess` directly attached. Users should use roles for admin access, not direct policy attachment.

Flag as **HIGH** if any user has `arn:aws:iam::aws:policy/PowerUserAccess` or any policy with `"Effect": "Allow", "Action": "*", "Resource": "*"`.

### Root Account Usage

Check the credential report for the root account row (first row, user is `<root_account>`). Flag as **CRITICAL** if:
- `mfa_active` is `false`
- `access_key_1_active` or `access_key_2_active` is `true` (root should not have access keys)
- `password_last_used` is recent (root should rarely be used)

## Step 3: S3 Security Checks

```
aws s3api list-buckets --query "Buckets[].Name" --output json
```

For each bucket:

### Public Access Block

```
aws s3api get-public-access-block --bucket <BUCKET> --output json 2>/dev/null
```

If this returns an error (no block configured) or any of the four settings (`BlockPublicAcls`, `IgnorePublicAcls`, `BlockPublicPolicy`, `RestrictPublicBuckets`) is `false`, flag as **HIGH: Public access block not fully enabled**.

### Bucket Policy Check

```
aws s3api get-bucket-policy --bucket <BUCKET> --output json 2>/dev/null
```

If the policy contains `"Principal": "*"` or `"Principal": {"AWS": "*"}` with `"Effect": "Allow"`, flag as **CRITICAL: Bucket policy allows public access**.

### Encryption Check

```
aws s3api get-bucket-encryption --bucket <BUCKET> --output json 2>/dev/null
```

If this returns an error (no encryption configured), flag as **MEDIUM: Server-side encryption not enabled**.

### Bucket ACL

```
aws s3api get-bucket-acl --bucket <BUCKET> --query "Grants[?Grantee.URI=='http://acs.amazonaws.com/groups/global/AllUsers' || Grantee.URI=='http://acs.amazonaws.com/groups/global/AuthenticatedUsers']" --output json
```

If any grants are returned, flag as **CRITICAL: Bucket ACL grants public or authenticated-users access**.

## Step 4: Security Group Checks

For each region:

```
aws ec2 describe-security-groups --region <REGION> --query "SecurityGroups[].{Id:GroupId,Name:GroupName,VpcId:VpcId,IngressRules:IpPermissions}" --output json
```

For each security group, check each ingress rule. Flag as:

- **CRITICAL**: Rule allows `0.0.0.0/0` or `::/0` on ports 22 (SSH), 3389 (RDP), 3306 (MySQL), 5432 (PostgreSQL), 1433 (MSSQL), 27017 (MongoDB), 6379 (Redis), 9200 (Elasticsearch)
- **HIGH**: Rule allows `0.0.0.0/0` on any port range greater than 100 ports
- **MEDIUM**: Rule allows `0.0.0.0/0` on ports 80 or 443 (acceptable for public web servers, but verify intentional)

For critical findings, also check which instances use the security group:

```
aws ec2 describe-instances --region <REGION> --filters "Name=instance.group-id,Values=<SG_ID>" --query "Reservations[].Instances[].{Id:InstanceId,State:State.Name,Name:Tags[?Key=='Name']|[0].Value}" --output json
```

## Step 5: RDS Security Checks

For each region:

```
aws rds describe-db-instances --region <REGION> --query "DBInstances[].{Id:DBInstanceIdentifier,Engine:Engine,Public:PubliclyAccessible,Encrypted:StorageEncrypted,AutoMinorUpgrade:AutoMinorVersionUpgrade,DeletionProtection:DeletionProtection,VpcSecurityGroups:VpcSecurityGroups[].VpcSecurityGroupId}" --output json
```

Flag:
- **CRITICAL**: `PubliclyAccessible` is `true`
- **HIGH**: `StorageEncrypted` is `false`
- **MEDIUM**: `AutoMinorVersionUpgrade` is `false`
- **MEDIUM**: `DeletionProtection` is `false` for production-looking instances (name contains "prod", "production", or "live")

## Step 6: Additional Checks

### CloudTrail Enabled

```
aws cloudtrail describe-trails --query "trailList[].{Name:Name,IsMultiRegion:IsMultiRegionTrail,IsLogging:HasCustomEventSelectors}" --output json
```

```
aws cloudtrail get-trail-status --name <TRAIL_NAME> --query "{IsLogging:IsLogging}" --output json
```

If no trails exist or none are logging, flag as **CRITICAL: CloudTrail is not enabled**.

### EBS Encryption Default

For each region:

```
aws ec2 get-ebs-encryption-by-default --region <REGION> --query "EbsEncryptionByDefault" --output text
```

If `false`, flag as **MEDIUM: EBS encryption by default is not enabled**.

## Step 7: Generate Security Report

Use `file_write` to create `security-audit-report.md`:

```markdown
# Security Audit Report

**Account:** <account-id>
**Date:** <date>
**Auditor:** Cyntr Security Audit Skill v1.0.0

## Executive Summary

- **Critical findings:** <count>
- **High findings:** <count>
- **Medium findings:** <count>
- **Low findings:** <count>

## Critical Findings (Immediate Action Required)

### FINDING-001: <title>
- **Severity:** CRITICAL
- **Resource:** <ARN or identifier>
- **Description:** <what is wrong>
- **Risk:** <what could happen if exploited>
- **Remediation:** <specific steps to fix>

### FINDING-002: ...

## High Findings

### FINDING-XXX: ...

## Medium Findings

### FINDING-XXX: ...

## Compliance Summary

| Check | Status | Details |
|-------|--------|---------|
| MFA on all users | PASS/FAIL | X of Y users have MFA |
| No public S3 buckets | PASS/FAIL | X buckets are public |
| Security groups locked down | PASS/FAIL | X rules allow 0.0.0.0/0 on sensitive ports |
| RDS not public | PASS/FAIL | X instances are publicly accessible |
| Encryption at rest | PASS/FAIL | X resources unencrypted |
| CloudTrail enabled | PASS/FAIL | ... |

## Remediation Priority

1. <most critical fix first>
2. <next>
3. <next>
```

Present the critical and high findings to the user first, along with the file path for the full report.
