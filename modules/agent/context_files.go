package agent

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultContextFileNames are the per-workspace files loaded into every chat's
// system context, mirroring the AGENTS.md / SOUL.md / TOOLS.md convention from
// OpenClaw and Hermes.
var DefaultContextFileNames = []string{"AGENTS.md", "CYNTR.md", "SOUL.md", "TOOLS.md"}

// ContextLoader reads per-(tenant, agent) context files from a workspace root
// and concatenates them for injection into the system prompt. Reads are
// cached by modification time so edits are hot-reloaded without a restart, and
// every resolved path is confined to the workspace root — a tenant/agent name
// that tries to escape via ".." is rejected.
type ContextLoader struct {
	root  string
	names []string

	mu    sync.Mutex
	cache map[string]cachedContextFile
}

type cachedContextFile struct {
	mtime   time.Time
	content string
}

// NewContextLoader builds a loader rooted at root. When names is empty the
// default set is used.
func NewContextLoader(root string, names ...string) *ContextLoader {
	if len(names) == 0 {
		names = DefaultContextFileNames
	}
	return &ContextLoader{root: root, names: names, cache: make(map[string]cachedContextFile)}
}

// Load returns the concatenated, header-labeled content of the context files
// for (tenant, agent). Missing files are skipped; a fully missing workspace,
// an unreadable file, or a path that escapes the root all yield "" with no
// error — context files are best-effort and must never fail a chat.
func (c *ContextLoader) Load(tenant, agent string) string {
	if c == nil || c.root == "" {
		return ""
	}
	dir, ok := c.safeDir(tenant, agent)
	if !ok {
		return ""
	}
	var b strings.Builder
	for _, name := range c.names {
		content := c.readCached(filepath.Join(dir, name))
		if strings.TrimSpace(content) == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("# ")
		b.WriteString(name)
		b.WriteString("\n\n")
		b.WriteString(strings.TrimRight(content, "\n"))
		b.WriteString("\n")
	}
	return b.String()
}

// safeDir resolves the per-(tenant, agent) directory and confirms it stays
// within the workspace root.
func (c *ContextLoader) safeDir(tenant, agent string) (string, bool) {
	rootAbs, err := filepath.Abs(c.root)
	if err != nil {
		return "", false
	}
	dir := filepath.Join(rootAbs, tenant, agent)
	rel, err := filepath.Rel(rootAbs, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return dir, true
}

// readCached returns the file's content, re-reading only when its mtime has
// changed since the last read.
func (c *ContextLoader) readCached(path string) string {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if cached, ok := c.cache[path]; ok && cached.mtime.Equal(info.ModTime()) {
		return cached.content
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	c.cache[path] = cachedContextFile{mtime: info.ModTime(), content: string(data)}
	return string(data)
}
