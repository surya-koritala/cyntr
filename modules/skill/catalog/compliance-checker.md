---
name: compliance-checker
description: Verify SOC 2, HIPAA, and PCI DSS compliance controls via infrastructure checks
version: 1.0.0
author: cyntr
tools:
  - name: shell_exec
  - name: http_request
  - name: file_read
  - name: file_write
---

# Compliance Status Checker

Verify the status of compliance controls for SOC 2, HIPAA, and PCI DSS by
running automated checks against infrastructure, configurations, and cloud
services. Produce a compliance matrix with pass, fail, or unknown status for
each control.

## Prerequisites

- Shell access is required for infrastructure checks.
- AWS CLI configured with appropriate read-only permissions (if checking AWS).
- The user should specify which frameworks to check: `soc2`, `hipaa`, `pci`, or
  `all`.
- This skill performs read-only checks. It never modifies infrastructure.

## Step 1: Determine Scope

Ask the user (or infer from their request):
- **Frameworks**: SOC 2, HIPAA, PCI DSS, or all.
- **Environment**: Production, staging, or specific AWS account/region.
- **Services**: Specific services to check, or all discoverable services.

Set the AWS region if applicable:

```
export AWS_DEFAULT_REGION=${AWS_REGION:-us-east-1}
```

Verify AWS CLI access:

```
aws sts get-caller-identity 2>/dev/null
```

If AWS is not available, mark all AWS-dependent checks as "Unknown - No AWS
access" and proceed with local checks only.

## Step 2: SOC 2 Controls

SOC 2 focuses on security, availability, processing integrity, confidentiality,
and privacy. Check the following controls:

### CC6.1 - Logical Access Controls

```
# Check IAM password policy
aws iam get-account-password-policy 2>/dev/null

# Check for MFA on root account
aws iam get-account-summary --query 'SummaryMap.AccountMFAEnabled' 2>/dev/null

# Check for users without MFA
aws iam generate-credential-report 2>/dev/null
aws iam get-credential-report --query 'Content' --output text 2>/dev/null | base64 -d | awk -F',' 'NR>1 && $4=="true" && $8=="false" { print $1 }'
```

Pass criteria: Password policy enforces 14+ characters, MFA required,
no users with console access lack MFA.

### CC6.6 - Encryption in Transit

```
# Check for HTTPS-only on load balancers
aws elbv2 describe-listeners --query 'Listeners[?Protocol==`HTTP`].[ListenerArn,Port]' 2>/dev/null

# Check SSL certificate validity
aws acm list-certificates --query 'CertificateSummaryList[*].[DomainName,Status]' 2>/dev/null
```

Pass criteria: No HTTP-only listeners on public-facing load balancers, all
certificates in ISSUED state.

### CC6.7 - Encryption at Rest

```
# Check S3 default encryption
aws s3api list-buckets --query 'Buckets[].Name' --output text 2>/dev/null | tr '\t' '\n' | while read bucket; do
  enc=$(aws s3api get-bucket-encryption --bucket "$bucket" 2>/dev/null)
  if [ -z "$enc" ]; then
    echo "FAIL: $bucket - no default encryption"
  else
    echo "PASS: $bucket"
  fi
done

# Check RDS encryption
aws rds describe-db-instances --query 'DBInstances[*].[DBInstanceIdentifier,StorageEncrypted]' --output table 2>/dev/null

# Check EBS default encryption
aws ec2 get-ebs-encryption-by-default --query 'EbsEncryptionByDefault' 2>/dev/null
```

Pass criteria: All S3 buckets have default encryption, all RDS instances
encrypted, EBS default encryption enabled.

### CC7.2 - Monitoring and Logging

```
# Check CloudTrail is enabled
aws cloudtrail describe-trails --query 'trailList[*].[Name,IsMultiRegionTrail,IsLogging]' --output table 2>/dev/null

# Check CloudTrail log validation
aws cloudtrail describe-trails --query 'trailList[*].[Name,LogFileValidationEnabled]' --output table 2>/dev/null

# Check CloudWatch alarms exist
aws cloudwatch describe-alarms --query 'MetricAlarms | length(@)' 2>/dev/null

# Check VPC flow logs
aws ec2 describe-flow-logs --query 'FlowLogs[*].[FlowLogId,ResourceId,FlowLogStatus]' --output table 2>/dev/null
```

Pass criteria: CloudTrail enabled with multi-region and log validation,
CloudWatch alarms configured, VPC flow logs active.

### CC8.1 - Change Management

```
# Check for protected branches
gh api repos/<owner>/<repo>/branches/main/protection 2>/dev/null

# Check for required PR reviews
gh api repos/<owner>/<repo>/branches/main/protection/required_pull_request_reviews 2>/dev/null
```

Pass criteria: Main branch protected, PR reviews required.

## Step 3: HIPAA Controls

HIPAA focuses on protecting health information (PHI). Check these controls:

### 164.312(a)(1) - Access Control

```
# Check for role-based access (IAM policies)
aws iam list-policies --scope Local --query 'Policies[*].[PolicyName,AttachmentCount]' --output table 2>/dev/null

# Check for overly permissive policies (admin access)
aws iam list-policies --scope Local --query 'Policies[].Arn' --output text 2>/dev/null | tr '\t' '\n' | while read arn; do
  aws iam get-policy-version --policy-arn "$arn" --version-id $(aws iam get-policy --policy-arn "$arn" --query 'Policy.DefaultVersionId' --output text 2>/dev/null) --query 'PolicyVersion.Document' --output json 2>/dev/null | grep -l '"Effect": "Allow".*"Action": "\*".*"Resource": "\*"'
done
```

Pass criteria: No wildcard admin policies attached to users/groups, least-
privilege policies in use.

### 164.312(a)(2)(iv) - Encryption

Run the same encryption checks as SOC 2 CC6.6 and CC6.7. Both in-transit
and at-rest encryption are required for PHI.

### 164.312(b) - Audit Controls

```
# Verify CloudTrail logging (same as SOC 2 CC7.2)
# Additionally check for access logging on S3 buckets containing PHI
aws s3api get-bucket-logging --bucket <phi_bucket> 2>/dev/null
```

Pass criteria: All SOC 2 logging checks pass, plus access logging on PHI
data stores.

### 164.312(c)(1) - Integrity Controls

```
# Check S3 versioning on PHI buckets
aws s3api get-bucket-versioning --bucket <phi_bucket> 2>/dev/null

# Check RDS automated backups
aws rds describe-db-instances --query 'DBInstances[*].[DBInstanceIdentifier,BackupRetentionPeriod]' --output table 2>/dev/null
```

Pass criteria: S3 versioning enabled on PHI buckets, RDS backup retention
of at least 7 days.

### 164.312(e)(1) - Transmission Security

```
# Check security groups for unencrypted database ports open to the internet
aws ec2 describe-security-groups --query 'SecurityGroups[*].IpPermissions[?FromPort==`3306` || FromPort==`5432` || FromPort==`27017`]' --output json 2>/dev/null | grep "0.0.0.0/0"
```

Pass criteria: No database ports open to 0.0.0.0/0.

## Step 4: PCI DSS Controls

PCI DSS focuses on payment card data security. Check these controls:

### Req 1 - Network Segmentation

```
# Check VPC configuration
aws ec2 describe-vpcs --query 'Vpcs[*].[VpcId,CidrBlock,IsDefault]' --output table 2>/dev/null

# Check for public subnets with direct internet access
aws ec2 describe-subnets --query 'Subnets[?MapPublicIpOnLaunch==`true`].[SubnetId,VpcId,CidrBlock]' --output table 2>/dev/null

# Check NACLs
aws ec2 describe-network-acls --query 'NetworkAcls[*].[NetworkAclId,VpcId]' --output table 2>/dev/null
```

Pass criteria: Cardholder data environment (CDE) in a separate VPC or subnet,
NACLs restricting traffic.

### Req 2 - Secure Configuration

```
# Check for default security groups in use
aws ec2 describe-security-groups --filters "Name=group-name,Values=default" --query 'SecurityGroups[?IpPermissions[0]].[GroupId,VpcId]' --output table 2>/dev/null
```

Pass criteria: Default security groups have no inbound rules.

### Req 3 - Key Management

```
# Check KMS keys
aws kms list-keys --query 'Keys[*].KeyId' --output text 2>/dev/null | tr '\t' '\n' | while read key; do
  aws kms describe-key --key-id "$key" --query 'KeyMetadata.[KeyId,KeyState,KeyManager,Origin]' --output table 2>/dev/null
done

# Check for key rotation
aws kms list-keys --query 'Keys[*].KeyId' --output text 2>/dev/null | tr '\t' '\n' | while read key; do
  aws kms get-key-rotation-status --key-id "$key" 2>/dev/null
done
```

Pass criteria: KMS keys in use, automatic key rotation enabled.

### Req 6 - Vulnerability Management

```
# Check for ECR image scanning
aws ecr describe-repositories --query 'repositories[*].[repositoryName,imageScanningConfiguration.scanOnPush]' --output table 2>/dev/null

# Check for recent vulnerability scan results
aws inspector2 list-findings --filter-criteria '{"severity":[{"comparison":"EQUALS","value":"CRITICAL"}]}' --query 'findings | length(@)' 2>/dev/null
```

Pass criteria: Image scanning enabled, no critical unresolved findings.

### Req 10 - Logging and Monitoring

Run the same logging checks as SOC 2 CC7.2. PCI DSS additionally requires:

```
# Verify log retention (at least 1 year, 3 months readily available)
aws logs describe-log-groups --query 'logGroups[*].[logGroupName,retentionInDays]' --output table 2>/dev/null
```

Pass criteria: All log groups have retention set, minimum 90 days.

## Step 5: Local Security Checks

Run checks that do not require cloud access:

```
# Check for secrets in code
grep -rn "password\|secret\|api_key\|private_key\|AWS_SECRET" --include="*.py" --include="*.js" --include="*.ts" --include="*.go" --include="*.java" --include="*.rb" --include="*.env" . 2>/dev/null | grep -v "node_modules\|.git\|vendor\|test\|spec\|mock" | head -20

# Check for .env files committed to git
git ls-files | grep -i "\.env$" 2>/dev/null

# Check for private keys in repo
git ls-files | grep -i "\.pem$\|\.key$\|id_rsa" 2>/dev/null

# Check Dockerfile security
grep -n "FROM.*:latest" Dockerfile 2>/dev/null
grep -n "USER root" Dockerfile 2>/dev/null
```

## Step 6: Generate Compliance Matrix

```
# Compliance Status Report
**Generated:** <current date>
**Frameworks:** <SOC 2 | HIPAA | PCI DSS | All>
**Environment:** <environment name>

## Executive Summary
- **Controls checked:** <count>
- **Passing:** <count> (<percentage>%)
- **Failing:** <count> (<percentage>%)
- **Unknown:** <count> (<percentage>%)

## Compliance Matrix

### SOC 2
| Control | Description | Status | Details |
|---------|-------------|--------|---------|
| CC6.1   | Logical Access | <PASS/FAIL/UNKNOWN> | <details> |
| CC6.6   | Encryption in Transit | <status> | <details> |
| CC6.7   | Encryption at Rest | <status> | <details> |
| CC7.2   | Monitoring | <status> | <details> |
| CC8.1   | Change Management | <status> | <details> |

### HIPAA
| Control | Description | Status | Details |
|---------|-------------|--------|---------|
| 164.312(a) | Access Control | <status> | <details> |
| 164.312(b) | Audit Controls | <status> | <details> |
| 164.312(c) | Integrity | <status> | <details> |
| 164.312(e) | Transmission Security | <status> | <details> |

### PCI DSS
| Control | Description | Status | Details |
|---------|-------------|--------|---------|
| Req 1  | Network Segmentation | <status> | <details> |
| Req 2  | Secure Config | <status> | <details> |
| Req 3  | Key Management | <status> | <details> |
| Req 6  | Vulnerability Mgmt | <status> | <details> |
| Req 10 | Logging | <status> | <details> |

## Critical Failures
<List all FAIL items with remediation steps>

## Remediation Priority
| # | Control | Framework | Effort | Impact | Recommendation |
|---|---------|-----------|--------|--------|----------------|
| 1 | <ctrl>  | <fw>      | <Low/Med/High> | <Low/Med/High> | <action> |

## Notes
- Controls marked UNKNOWN could not be verified due to access limitations.
- This report is an automated point-in-time assessment, not a formal audit.
- Consult your compliance team for official audit preparation.

---
*Generated by cyntr/compliance-checker v1.0.0*
```

## Step 7: Output

Write the report to file (suggested: `compliance-report-<date>.md`).
Return the full report as the response.

Warn the user: "This automated check covers a subset of each framework's full
control set. It should supplement, not replace, a formal compliance audit."
