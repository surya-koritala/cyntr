package mcp

import "strings"

// MCPCatalogEntry represents an MCP server available in the marketplace.
type MCPCatalogEntry struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Command     string            `json:"command,omitempty"`
	Args        []string          `json:"args,omitempty"`
	URL         string            `json:"url,omitempty"`
	Transport   string            `json:"transport"`
	Env         map[string]string `json:"env,omitempty"`
	Source      string            `json:"source"`
	Homepage    string            `json:"homepage,omitempty"`
}

var BuiltinMCPCatalog = []MCPCatalogEntry{
	{
		Name:        "filesystem",
		Description: "Read, write, and search files on the local filesystem",
		Command:     "npx", Args: []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
		Transport: "stdio", Source: "builtin",
		Homepage: "https://github.com/modelcontextprotocol/servers",
	},
	{
		Name:        "github",
		Description: "Interact with GitHub repositories, issues, and pull requests",
		Command:     "npx", Args: []string{"-y", "@modelcontextprotocol/server-github"},
		Transport: "stdio", Source: "builtin",
		Env: map[string]string{"GITHUB_PERSONAL_ACCESS_TOKEN": ""},
	},
	{
		Name:        "postgres",
		Description: "Query PostgreSQL databases with read-only access",
		Command:     "npx", Args: []string{"-y", "@modelcontextprotocol/server-postgres"},
		Transport: "stdio", Source: "builtin",
		Env: map[string]string{"DATABASE_URL": ""},
	},
	{
		Name:        "slack",
		Description: "Send and read Slack messages, manage channels",
		Command:     "npx", Args: []string{"-y", "@modelcontextprotocol/server-slack"},
		Transport: "stdio", Source: "builtin",
		Env: map[string]string{"SLACK_BOT_TOKEN": ""},
	},
	{
		Name:        "brave-search",
		Description: "Web search via Brave Search API",
		Command:     "npx", Args: []string{"-y", "@modelcontextprotocol/server-brave-search"},
		Transport: "stdio", Source: "builtin",
		Env: map[string]string{"BRAVE_API_KEY": ""},
	},
	{
		Name:        "memory",
		Description: "Persistent memory using a knowledge graph",
		Command:     "npx", Args: []string{"-y", "@modelcontextprotocol/server-memory"},
		Transport: "stdio", Source: "builtin",
	},
	{
		Name:        "sqlite",
		Description: "Query and analyze SQLite databases",
		Command:     "npx", Args: []string{"-y", "@modelcontextprotocol/server-sqlite"},
		Transport: "stdio", Source: "builtin",
	},
	{
		Name:        "puppeteer",
		Description: "Browser automation with Puppeteer",
		Command:     "npx", Args: []string{"-y", "@modelcontextprotocol/server-puppeteer"},
		Transport: "stdio", Source: "builtin",
	},
}

func SearchBuiltinMCPCatalog(query string) []MCPCatalogEntry {
	if query == "" {
		return BuiltinMCPCatalog
	}
	query = strings.ToLower(query)
	var results []MCPCatalogEntry
	for _, e := range BuiltinMCPCatalog {
		if strings.Contains(strings.ToLower(e.Name), query) ||
			strings.Contains(strings.ToLower(e.Description), query) {
			results = append(results, e)
		}
	}
	return results
}
