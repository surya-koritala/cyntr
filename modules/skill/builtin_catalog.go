package skill

import "strings"

// BuiltinCatalog contains curated skills that ship with Cyntr.
var BuiltinCatalog = []MarketplaceEntry{
	{
		Name:        "cloud-diagnostics",
		Version:     "1.0.0",
		Author:      "cyntr",
		Description: "Diagnose AWS, Azure, and GCP infrastructure issues. Includes runbooks for common problems like high CPU, disk full, unhealthy targets, and failed deployments.",
		DownloadURL: "https://raw.githubusercontent.com/surya-koritala/cyntr-skills/main/cloud-diagnostics/skill.yaml",
	},
	{
		Name:        "code-review",
		Version:     "1.0.0",
		Author:      "cyntr",
		Description: "Review code changes for bugs, security issues, and best practices. Supports Go, Python, JavaScript, TypeScript, and Java.",
		DownloadURL: "https://raw.githubusercontent.com/surya-koritala/cyntr-skills/main/code-review/skill.yaml",
	},
	{
		Name:        "incident-response",
		Version:     "1.0.0",
		Author:      "cyntr",
		Description: "Structured incident response workflow. Detect, diagnose, mitigate, and document incidents with automated runbook execution.",
		DownloadURL: "https://raw.githubusercontent.com/surya-koritala/cyntr-skills/main/incident-response/skill.yaml",
	},
	{
		Name:        "document-summarizer",
		Version:     "1.0.0",
		Author:      "cyntr",
		Description: "Summarize PDFs, web pages, and documents. Extract key points, action items, and decisions.",
		DownloadURL: "https://raw.githubusercontent.com/surya-koritala/cyntr-skills/main/document-summarizer/skill.yaml",
	},
	{
		Name:        "data-analyst",
		Version:     "1.0.0",
		Author:      "cyntr",
		Description: "Query databases, analyze CSV files, and generate reports with charts. Supports SQLite and PostgreSQL.",
		DownloadURL: "https://raw.githubusercontent.com/surya-koritala/cyntr-skills/main/data-analyst/skill.yaml",
	},
	{
		Name:        "customer-support",
		Version:     "1.0.0",
		Author:      "cyntr",
		Description: "Handle customer inquiries, search knowledge base, escalate issues. Integrates with Jira for ticket creation.",
		DownloadURL: "https://raw.githubusercontent.com/surya-koritala/cyntr-skills/main/customer-support/skill.yaml",
	},
	{
		Name:        "security-scanner",
		Version:     "1.0.0",
		Author:      "cyntr",
		Description: "Scan repositories and infrastructure for security vulnerabilities. Check for exposed secrets, outdated dependencies, and misconfigurations.",
		DownloadURL: "https://raw.githubusercontent.com/surya-koritala/cyntr-skills/main/security-scanner/skill.yaml",
	},
	{
		Name:        "api-tester",
		Version:     "1.0.0",
		Author:      "cyntr",
		Description: "Test REST APIs automatically. Generate test cases from OpenAPI specs, run requests, validate responses.",
		DownloadURL: "https://raw.githubusercontent.com/surya-koritala/cyntr-skills/main/api-tester/skill.yaml",
	},
	// Cyntr for Cloud Ops — vertical pack (ships in-tree under skills/cloud-ops/)
	{
		Name:        "cost-anomaly-investigator",
		Version:     "1.0.0",
		Author:      "cyntr",
		Description: "Investigates AWS cost spikes end-to-end: Cost Explorer for top services/accounts driving the increase, then drills into specific services (EC2, S3, RDS, NAT, data transfer) via the AWS CLI. Returns a triage with dollar amounts, dates, and a recommended read-only action.",
		DownloadURL: "inproc:skills/cloud-ops/cost-anomaly-investigator",
		Source:      "builtin",
	},
	{
		Name:        "k8s-troubleshooter",
		Version:     "1.0.0",
		Author:      "cyntr",
		Description: "Diagnoses Kubernetes workload problems (CrashLoopBackOff, Pending, ImagePullBackOff, OOMKilled) using read-only kubectl. Walks get → describe → logs → events, then summarizes root cause and a single remediation step for human approval.",
		DownloadURL: "inproc:skills/cloud-ops/k8s-troubleshooter",
		Source:      "builtin",
	},
	{
		Name:        "security-audit-runner",
		Version:     "1.0.0",
		Author:      "cyntr",
		Description: "Runs point-in-time AWS security audits: IAM MFA coverage, S3 public access, security group ingress on sensitive ports, root account hygiene. Output is a P0/P1/P2 findings list with runbook citations.",
		DownloadURL: "inproc:skills/cloud-ops/security-audit-runner",
		Source:      "builtin",
	},
	// Verified OpenClaw community skills
	{
		Name:        "openclaw-weather-checker",
		Version:     "2.1.0",
		Author:      "openclaw-community",
		Description: "Check weather for any city using wttr.in API. Uses http_request tool to fetch and present weather data in a friendly format.",
		DownloadURL: "openclaw:weather",
		Source:      "openclaw",
	},
	{
		Name:        "openclaw-code-reviewer",
		Version:     "1.5.0",
		Author:      "dev-tools-org",
		Description: "Expert code reviewer. Reads files, analyzes for bugs, security issues, and style problems. Integrates with GitHub for PR reviews.",
		DownloadURL: "openclaw:code-review",
		Source:      "openclaw",
	},
	{
		Name:        "openclaw-doc-writer",
		Version:     "1.0.0",
		Author:      "community",
		Description: "Generate documentation from source code. Searches for source files, reads them, and writes markdown docs automatically.",
		DownloadURL: "openclaw:doc-writer",
		Source:      "openclaw",
	},
	{
		Name:        "openclaw-cyntr-security",
		Version:     "0.1.0",
		Author:      "cyntr",
		Description: "Security and audit layer for AI agents. Routes dangerous actions through Cyntr's permission engine. Logs all actions for audit compliance.",
		DownloadURL: "openclaw:cyntr",
		Source:      "openclaw",
	},
}

// SearchBuiltinCatalog filters the built-in catalog by query string.
func SearchBuiltinCatalog(query string) []MarketplaceEntry {
	if query == "" {
		return BuiltinCatalog
	}
	query = strings.ToLower(query)
	var results []MarketplaceEntry
	for _, entry := range BuiltinCatalog {
		if strings.Contains(strings.ToLower(entry.Name), query) ||
			strings.Contains(strings.ToLower(entry.Description), query) ||
			strings.Contains(strings.ToLower(entry.Author), query) {
			results = append(results, entry)
		}
	}
	return results
}
