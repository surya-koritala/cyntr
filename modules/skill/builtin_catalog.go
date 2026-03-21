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
