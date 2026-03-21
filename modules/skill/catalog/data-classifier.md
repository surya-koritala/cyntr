---
name: data-classifier
description: Scan files for PII, PHI, and financial data patterns and classify sensitivity levels
version: 1.0.0
author: cyntr
tools:
  - name: file_read
  - name: file_search
  - name: shell_exec
  - name: file_write
---

# Data Sensitivity Classifier

Scan files and directories for sensitive data patterns including personally
identifiable information (PII), protected health information (PHI), and
financial data. Classify each file by sensitivity level and generate a
comprehensive data classification report.

## Prerequisites

- The user must provide a directory path or list of files to scan.
- Shell access is required for recursive scanning.
- This skill only reads files; it never modifies them.

## Step 1: Determine Scan Scope

If the user provides a directory, enumerate scannable files:

```
find <directory> -type f \
  -not -path '*/.git/*' \
  -not -path '*/node_modules/*' \
  -not -path '*/vendor/*' \
  -not -path '*/__pycache__/*' \
  -not -path '*/dist/*' \
  -not -path '*/build/*' \
  -not -name '*.min.js' \
  -not -name '*.min.css' \
  -not -name '*.map' \
  -not -name '*.lock' \
  -not -name '*.sum' \
  \( -name '*.py' -o -name '*.js' -o -name '*.ts' -o -name '*.java' \
     -o -name '*.go' -o -name '*.rb' -o -name '*.php' -o -name '*.cs' \
     -o -name '*.swift' -o -name '*.kt' -o -name '*.rs' \
     -o -name '*.json' -o -name '*.yaml' -o -name '*.yml' \
     -o -name '*.xml' -o -name '*.csv' -o -name '*.tsv' \
     -o -name '*.txt' -o -name '*.md' -o -name '*.log' \
     -o -name '*.sql' -o -name '*.env' -o -name '*.conf' \
     -o -name '*.cfg' -o -name '*.ini' -o -name '*.properties' \
     -o -name '*.html' -o -name '*.htm' \) \
  2>/dev/null
```

Count the total files to scan. If more than 1,000 files, inform the user and
consider sampling or limiting to specific directories. For very large files
(>1 MB), scan only the first 10,000 lines.

Record the scan manifest: total files, total size, file type distribution.

## Step 2: Define Detection Patterns

Use the following regex patterns for each data category. All patterns are
applied per-line using `grep -Pn` (or `grep -En` on systems without PCRE).

### PII Patterns

| Pattern Name | Regex | Description |
|-------------|-------|-------------|
| Email | `[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}` | Email addresses |
| US Phone | `(\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}` | US phone numbers |
| SSN | `\b\d{3}[-]?\d{2}[-]?\d{4}\b` | Social Security Numbers |
| US Address | `\b\d+\s+[A-Z][a-zA-Z]+\s+(St|Ave|Blvd|Dr|Rd|Ln|Way|Ct|Pl|Cir)\b` | Street addresses |
| Date of Birth | `\b(DOB|date.of.birth|birth.?date|born)\b` | Date of birth references |
| Driver License | `\b(DL|driver.?license|license.?number)\s*[:=]?\s*[A-Z0-9]+\b` | Driver license references |
| Passport | `\b(passport)\s*[:=]?\s*[A-Z0-9]{6,9}\b` | Passport number references |
| IP Address | `\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b` | IPv4 addresses (context-dependent) |
| Name Fields | `\b(first.?name|last.?name|full.?name|surname|given.?name)\b` | Named fields holding personal names |

### PHI Patterns

| Pattern Name | Regex | Description |
|-------------|-------|-------------|
| MRN | `\b(MRN|medical.?record|patient.?id|chart.?number)\s*[:=]?\s*[A-Z0-9]+\b` | Medical record numbers |
| Diagnosis | `\b(ICD[-.]?1[0-9]|diagnosis|diagnos)\b` | Diagnosis code references |
| Patient Name | `\b(patient.?name|patient)\s*[:=]` | Patient name fields |
| Medication | `\b(medication|prescription|rx|dosage|pharma)\b` | Medication references |
| Lab Results | `\b(lab.?result|test.?result|blood.?type|hemoglobin|glucose)\b` | Laboratory data |
| Insurance ID | `\b(insurance.?id|policy.?number|member.?id|group.?number)\b` | Health insurance identifiers |
| Provider NPI | `\b(NPI|provider.?id)\s*[:=]?\s*\d{10}\b` | National Provider Identifier |

### Financial Data Patterns

| Pattern Name | Regex | Description |
|-------------|-------|-------------|
| Credit Card | `\b(?:4\d{3}|5[1-5]\d{2}|3[47]\d{2}|6(?:011|5\d{2}))[- ]?\d{4}[- ]?\d{4}[- ]?\d{4}\b` | Visa, MC, Amex, Discover |
| Bank Account | `\b(account.?number|acct.?no|bank.?account)\s*[:=]?\s*\d{8,17}\b` | Bank account references |
| Routing Number | `\b(routing|ABA)\s*[:=]?\s*\d{9}\b` | US bank routing numbers |
| Tax ID / EIN | `\b(EIN|tax.?id|TIN)\s*[:=]?\s*\d{2}[-]?\d{7}\b` | Employer/Tax ID numbers |
| IBAN | `\b[A-Z]{2}\d{2}[A-Z0-9]{4}\d{7}([A-Z0-9]?\d{0,16})?\b` | International bank accounts |
| CVV | `\b(CVV|CVC|CVV2|security.?code)\s*[:=]?\s*\d{3,4}\b` | Card verification values |

### Credential Patterns

| Pattern Name | Regex | Description |
|-------------|-------|-------------|
| API Key | `\b(api[_-]?key|apikey)\s*[:=]\s*['"]?[A-Za-z0-9_-]{20,}\b` | API keys |
| Secret | `\b(secret|password|passwd|pwd)\s*[:=]\s*['"]?[^\s'"]{8,}\b` | Secrets and passwords |
| Private Key | `-----BEGIN (RSA |EC |DSA )?PRIVATE KEY-----` | PEM private keys |
| AWS Key | `\bAKIA[0-9A-Z]{16}\b` | AWS access key IDs |
| JWT | `\beyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\b` | JSON Web Tokens |
| Connection String | `\b(postgres|mysql|mongodb|redis):\/\/[^\s]+\b` | Database connection strings |

## Step 3: Execute Scan

For each file in the scan manifest, run pattern matching. Use `grep` for
efficiency:

```
# Scan a single file against all patterns, output matching lines with context
for pattern_name in "${!patterns[@]}"; do
  matches=$(grep -Pcn "${patterns[$pattern_name]}" "$file" 2>/dev/null)
  if [ "$matches" -gt 0 ]; then
    echo "$file|$pattern_name|$matches"
    grep -Pn "${patterns[$pattern_name]}" "$file" 2>/dev/null | head -5
  fi
done
```

For performance on large scans, batch files and process in parallel groups.
Alternatively, use a single combined grep per file:

```
grep -Pcn 'PATTERN1|PATTERN2|PATTERN3' "$file" 2>/dev/null
```

For each match, record:
- File path
- Line number
- Pattern that matched
- The matching text (redact actual sensitive values in the report by showing
  only the first and last 2 characters, e.g., `jo****@e****.com`)
- Surrounding context (1 line before and after for classification accuracy)

## Step 4: Reduce False Positives

Apply these heuristic filters to reduce noise:

1. **Test files**: Matches in files containing `test`, `spec`, `mock`, `fixture`,
   `sample`, `example` in their path are likely test data. Flag them separately
   as "test data" rather than real sensitive data.

2. **Comments and documentation**: Matches inside code comments (`//`, `#`,
   `/* */`, `"""`) or markdown files may be documentation references, not real
   data. Flag with lower confidence.

3. **Regex/validation code**: Matches inside regex patterns (between `/` or
   in a regex constructor) are likely validation logic, not data. Exclude these.

4. **IP addresses**: `127.0.0.1`, `0.0.0.0`, `localhost`, and `10.x.x.x`,
   `172.16-31.x.x`, `192.168.x.x` are private/local. Flag only public IPs.

5. **SSN pattern**: `\d{3}-\d{2}-\d{4}` also matches dates and other numeric
   patterns. Require surrounding context that suggests it is an SSN (e.g., the
   word "SSN", "social security", or a field name containing "ssn").

6. **Email patterns in package files**: `package.json`, `*.lock`, `go.sum` often
   contain maintainer emails. Flag these as low-risk.

Record a confidence level for each finding: High, Medium, or Low.

## Step 5: Classify Files

Assign each file a sensitivity classification based on its findings:

| Level | Criteria | Examples |
|-------|----------|---------|
| **Restricted** | Contains SSNs, credit card numbers, private keys, passwords, or PHI with patient identifiers | Database dumps, key files, credential configs |
| **Confidential** | Contains PII (emails, phones, names) or financial references (account numbers, tax IDs) | Customer data files, HR records, financial reports |
| **Internal** | Contains IP addresses, internal URLs, non-sensitive business data, or test data with PII patterns | Config files, test fixtures, internal docs |
| **Public** | No sensitive patterns detected | Open-source code, public documentation |

A file's classification is determined by its highest-sensitivity finding. One
SSN match makes the entire file Restricted, regardless of other content.

## Step 6: Generate Summary Statistics

Compute:
- Total files scanned
- Files per classification level
- Total findings per pattern category (PII, PHI, Financial, Credential)
- Top 10 files by finding count
- Most common pattern types found
- Findings in test vs. production code

## Step 7: Generate Report

```
# Data Classification Report
**Scan Directory:** <path>
**Generated:** <current date>

## Executive Summary
- **Files scanned:** <count>
- **Files with findings:** <count> (<pct>%)
- **Total findings:** <count>
- **Highest sensitivity found:** <Restricted|Confidential|Internal|Public>

## Classification Distribution
| Level | Files | % of Scanned |
|-------|-------|-------------|
| Restricted | <n> | <pct>% |
| Confidential | <n> | <pct>% |
| Internal | <n> | <pct>% |
| Public | <n> | <pct>% |

## Findings by Category
| Category | Pattern | Files | Matches | Confidence |
|----------|---------|-------|---------|------------|
| PII | Email | <n> | <n> | High |
| PII | SSN | <n> | <n> | High |
| PHI | MRN | <n> | <n> | Medium |
| Financial | Credit Card | <n> | <n> | High |
| Credential | API Key | <n> | <n> | High |
...

## Restricted Files (Immediate Attention Required)
| File | Findings | Top Pattern | Classification |
|------|----------|-------------|----------------|
<rows, sorted by finding count descending>

## Confidential Files
| File | Findings | Top Pattern | Classification |
|------|----------|-------------|----------------|
<rows>

## Detailed Findings

### <File Path>
**Classification:** <level>
**Findings:** <count>
| Line | Pattern | Match (Redacted) | Confidence |
|------|---------|-------------------|------------|
| <n>  | <type>  | <redacted value>  | <H/M/L>    |

<repeat for top 20 files by severity>

## Test Data Findings
<Separate section for findings in test/fixture files>
| File | Findings | Note |
|------|----------|------|
<rows>

## Recommendations
1. **Restricted files** should be encrypted at rest and access-controlled.
2. **Credential files** (.env, key files) should be added to .gitignore and
   rotated if committed to version control.
3. **PII in source code** should be moved to secure configuration or vault.
4. **Test fixtures** containing real PII patterns should use synthetic data.
5. <Additional specific recommendations based on findings>

## Limitations
- Pattern matching is heuristic; false positives and negatives are possible.
- Binary files, images, and encrypted files are not scanned.
- This scan does not assess data in databases, APIs, or runtime memory.

---
*Generated by cyntr/data-classifier v1.0.0*
```

## Step 8: Output

Write the report to file (suggested: `data-classification-<date>.md`).
Return the full report as the response.

If Restricted or Confidential files are found, prominently warn the user at
the start of the response and recommend immediate review of those files.
